package traffic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUnauthorizedAgent   = errors.New("unauthorized agent")
	ErrAgentServerRequired = errors.New("agent must be bound to a server")
	ErrVPNAccountNotFound  = errors.New("vpn account not found")
)

type Repository struct {
	pool *pgxpool.Pool
}

type reportingAgent struct {
	ID       string
	ServerID string
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) ReportUsage(ctx context.Context, tokenHash string, events []CreateUsageEventInput) (TrafficUsageReport, error) {
	agent, err := r.reportingAgentByTokenHash(ctx, tokenHash)
	if err != nil {
		return TrafficUsageReport{}, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return TrafficUsageReport{}, err
	}
	defer tx.Rollback(ctx)

	for _, event := range events {
		metadata := event.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			return TrafficUsageReport{}, err
		}

		result, err := tx.Exec(ctx, `
			INSERT INTO traffic_usage_events (
				server_id,
				agent_id,
				vpn_account_id,
				rx_bytes,
				tx_bytes,
				observed_at,
				metadata
			)
			SELECT
				$1::uuid,
				$2::uuid,
				a.id,
				$4,
				$5,
				$6,
				$7::jsonb
			FROM vpn_accounts a
			WHERE a.id = $3::uuid
			  AND a.server_id = $1::uuid
		`, agent.ServerID, agent.ID, event.VPNAccountID, event.RxBytes, event.TxBytes, event.ObservedAt, string(metadataBytes))
		if err != nil {
			return TrafficUsageReport{}, err
		}
		if result.RowsAffected() == 0 {
			return TrafficUsageReport{}, ErrVPNAccountNotFound
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE agents
		SET last_seen_at = now(), status = 'online', updated_at = now()
		WHERE id = $1::uuid
	`, agent.ID); err != nil {
		return TrafficUsageReport{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TrafficUsageReport{}, err
	}

	return TrafficUsageReport{OK: true, AgentID: agent.ID, ServerID: agent.ServerID, Accepted: len(events)}, nil
}

func (r *Repository) GetUsageSummary(ctx context.Context, vpnAccountID string, from time.Time, to time.Time) (TrafficUsageSummary, error) {
	var accountID string
	if err := r.pool.QueryRow(ctx, `
		SELECT id::text
		FROM vpn_accounts
		WHERE id = $1::uuid
	`, vpnAccountID).Scan(&accountID); errors.Is(err, pgx.ErrNoRows) {
		return TrafficUsageSummary{}, ErrVPNAccountNotFound
	} else if err != nil {
		return TrafficUsageSummary{}, err
	}

	summary := TrafficUsageSummary{
		VPNAccountID: accountID,
		Period: TrafficPeriod{
			From: from,
			To:   to,
		},
	}

	if err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(rx_bytes), 0)::bigint,
			COALESCE(SUM(tx_bytes), 0)::bigint
		FROM traffic_usage_events
		WHERE vpn_account_id = $1::uuid
		  AND observed_at >= $2
		  AND observed_at < $3
	`, accountID, from, to).Scan(&summary.Usage.RxBytes, &summary.Usage.TxBytes); err != nil {
		return TrafficUsageSummary{}, err
	}
	summary.Usage.TotalBytes = summary.Usage.RxBytes + summary.Usage.TxBytes

	limit, err := r.GetLimit(ctx, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return summary, nil
	}
	if err != nil {
		return TrafficUsageSummary{}, err
	}
	summary.Limit = buildLimitState(limit, summary.Usage.TotalBytes)

	return summary, nil
}

func (r *Repository) GetLimit(ctx context.Context, vpnAccountID string) (TrafficLimit, error) {
	return scanTrafficLimit(r.pool.QueryRow(ctx, `
		SELECT
			vpn_account_id::text,
			monthly_limit_bytes,
			hard_limit_enabled,
			speed_limit_bps,
			reset_day,
			created_at,
			updated_at
		FROM traffic_limits
		WHERE vpn_account_id = $1::uuid
	`, vpnAccountID))
}

func (r *Repository) UpsertLimit(ctx context.Context, vpnAccountID string, input UpsertTrafficLimitInput) (TrafficLimit, error) {
	resetDay := input.ResetDay
	if resetDay == 0 {
		resetDay = DefaultResetDay
	}

	limit, err := scanTrafficLimit(r.pool.QueryRow(ctx, `
		INSERT INTO traffic_limits (
			vpn_account_id,
			monthly_limit_bytes,
			hard_limit_enabled,
			speed_limit_bps,
			reset_day
		)
		SELECT
			a.id,
			$2,
			$3,
			$4,
			$5
		FROM vpn_accounts a
		WHERE a.id = $1::uuid
		ON CONFLICT (vpn_account_id)
		DO UPDATE SET
			monthly_limit_bytes = EXCLUDED.monthly_limit_bytes,
			hard_limit_enabled = EXCLUDED.hard_limit_enabled,
			speed_limit_bps = EXCLUDED.speed_limit_bps,
			reset_day = EXCLUDED.reset_day,
			updated_at = now()
		RETURNING
			vpn_account_id::text,
			monthly_limit_bytes,
			hard_limit_enabled,
			speed_limit_bps,
			reset_day,
			created_at,
			updated_at
	`, vpnAccountID, input.MonthlyLimitBytes, input.HardLimitEnabled, input.SpeedLimitBps, resetDay))
	if errors.Is(err, pgx.ErrNoRows) {
		return TrafficLimit{}, ErrVPNAccountNotFound
	}
	return limit, err
}

func (r *Repository) reportingAgentByTokenHash(ctx context.Context, tokenHash string) (reportingAgent, error) {
	var agent reportingAgent
	var serverID sql.NullString

	err := r.pool.QueryRow(ctx, `
		SELECT id::text, server_id::text
		FROM agents
		WHERE token_hash = $1
		  AND status <> 'disabled'
	`, tokenHash).Scan(&agent.ID, &serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return reportingAgent{}, ErrUnauthorizedAgent
	}
	if err != nil {
		return reportingAgent{}, err
	}
	if !serverID.Valid || serverID.String == "" {
		return reportingAgent{}, ErrAgentServerRequired
	}
	agent.ServerID = serverID.String
	return agent, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTrafficLimit(row scanner) (TrafficLimit, error) {
	var limit TrafficLimit
	var monthlyLimitBytes sql.NullInt64
	var speedLimitBps sql.NullInt64

	err := row.Scan(
		&limit.VPNAccountID,
		&monthlyLimitBytes,
		&limit.HardLimitEnabled,
		&speedLimitBps,
		&limit.ResetDay,
		&limit.CreatedAt,
		&limit.UpdatedAt,
	)
	if err != nil {
		return TrafficLimit{}, err
	}
	if monthlyLimitBytes.Valid {
		value := monthlyLimitBytes.Int64
		limit.MonthlyLimitBytes = &value
	}
	if speedLimitBps.Valid {
		value := speedLimitBps.Int64
		limit.SpeedLimitBps = &value
	}
	return limit, nil
}

func buildLimitState(limit TrafficLimit, totalBytes int64) *TrafficLimitState {
	state := &TrafficLimitState{
		MonthlyLimitBytes: limit.MonthlyLimitBytes,
		HardLimitEnabled:  limit.HardLimitEnabled,
		SpeedLimitBps:     limit.SpeedLimitBps,
		ResetDay:          limit.ResetDay,
		UpdatedAt:         limit.UpdatedAt,
	}
	if limit.MonthlyLimitBytes != nil && *limit.MonthlyLimitBytes > 0 {
		usedPercent := float64(totalBytes) / float64(*limit.MonthlyLimitBytes) * 100
		state.UsedPercent = &usedPercent
		state.LimitReached = totalBytes >= *limit.MonthlyLimitBytes
	}
	return state
}
