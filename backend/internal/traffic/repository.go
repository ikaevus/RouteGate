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

	affectedAccounts := map[string]struct{}{}
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
		affectedAccounts[event.VPNAccountID] = struct{}{}
	}

	for vpnAccountID := range affectedAccounts {
		if err := r.evaluateAccountLimitTx(ctx, tx, vpnAccountID); err != nil {
			return TrafficUsageReport{}, err
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
	return scanTrafficLimit(r.pool.QueryRow(ctx, trafficLimitSelectSQL+`
		WHERE vpn_account_id = $1::uuid
	`, vpnAccountID))
}

func (r *Repository) UpsertLimit(ctx context.Context, vpnAccountID string, input UpsertTrafficLimitInput) (TrafficLimit, error) {
	resetDay := input.ResetDay
	if resetDay == 0 {
		resetDay = DefaultResetDay
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return TrafficLimit{}, err
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
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
	`, vpnAccountID, input.MonthlyLimitBytes, input.HardLimitEnabled, input.SpeedLimitBps, resetDay)
	if err != nil {
		return TrafficLimit{}, err
	}
	if result.RowsAffected() == 0 {
		return TrafficLimit{}, ErrVPNAccountNotFound
	}

	if err := r.evaluateAccountLimitTx(ctx, tx, vpnAccountID); err != nil {
		return TrafficLimit{}, err
	}

	limit, err := scanTrafficLimit(tx.QueryRow(ctx, trafficLimitSelectSQL+`
		WHERE vpn_account_id = $1::uuid
	`, vpnAccountID))
	if err != nil {
		return TrafficLimit{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TrafficLimit{}, err
	}

	return limit, nil
}

func (r *Repository) evaluateAccountLimitTx(ctx context.Context, tx pgx.Tx, vpnAccountID string) error {
	_, err := tx.Exec(ctx, `
		WITH usage_totals AS (
			SELECT
				$1::uuid AS vpn_account_id,
				COALESCE(SUM(total_bytes), 0)::bigint AS total_bytes
			FROM traffic_usage_events
			WHERE vpn_account_id = $1::uuid
		), next_state AS (
			SELECT
				l.vpn_account_id,
				CASE
					WHEN l.hard_limit_enabled
					 AND l.monthly_limit_bytes IS NOT NULL
					 AND l.monthly_limit_bytes > 0
					 AND u.total_bytes >= l.monthly_limit_bytes
						THEN 'over_limit'
					WHEN l.hard_limit_enabled
					 AND l.monthly_limit_bytes IS NOT NULL
					 AND l.monthly_limit_bytes > 0
						THEN 'within_limit'
					ELSE 'not_enforced'
				END AS enforcement_status,
				CASE
					WHEN l.hard_limit_enabled
					 AND l.monthly_limit_bytes IS NOT NULL
					 AND l.monthly_limit_bytes > 0
					 AND u.total_bytes >= l.monthly_limit_bytes
						THEN COALESCE(l.limit_exceeded_at, now())
					ELSE NULL
				END AS limit_exceeded_at
			FROM traffic_limits l
			JOIN usage_totals u ON u.vpn_account_id = l.vpn_account_id
		)
		UPDATE traffic_limits l
		SET
			limit_exceeded_at = s.limit_exceeded_at,
			enforcement_status = s.enforcement_status,
			enforcement_updated_at = now(),
			updated_at = now()
		FROM next_state s
		WHERE l.vpn_account_id = s.vpn_account_id
		  AND (
			l.limit_exceeded_at IS DISTINCT FROM s.limit_exceeded_at
			OR l.enforcement_status IS DISTINCT FROM s.enforcement_status
		  )
	`, vpnAccountID)
	return err
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

const trafficLimitSelectSQL = `
	SELECT
		vpn_account_id::text,
		monthly_limit_bytes,
		hard_limit_enabled,
		speed_limit_bps,
		reset_day,
		limit_exceeded_at,
		enforcement_status,
		enforcement_updated_at,
		created_at,
		updated_at
	FROM traffic_limits
`

func scanTrafficLimit(row scanner) (TrafficLimit, error) {
	var limit TrafficLimit
	var monthlyLimitBytes sql.NullInt64
	var speedLimitBps sql.NullInt64
	var limitExceededAt sql.NullTime
	var enforcementStatus sql.NullString
	var enforcementUpdatedAt sql.NullTime

	err := row.Scan(
		&limit.VPNAccountID,
		&monthlyLimitBytes,
		&limit.HardLimitEnabled,
		&speedLimitBps,
		&limit.ResetDay,
		&limitExceededAt,
		&enforcementStatus,
		&enforcementUpdatedAt,
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
	if limitExceededAt.Valid {
		value := limitExceededAt.Time
		limit.LimitExceededAt = &value
	}
	if enforcementStatus.Valid && enforcementStatus.String != "" {
		limit.EnforcementStatus = enforcementStatus.String
	} else {
		limit.EnforcementStatus = TrafficLimitEnforcementNotEnforced
	}
	if enforcementUpdatedAt.Valid {
		value := enforcementUpdatedAt.Time
		limit.EnforcementUpdatedAt = &value
	}
	return limit, nil
}

func buildLimitState(limit TrafficLimit, totalBytes int64) *TrafficLimitState {
	state := &TrafficLimitState{
		MonthlyLimitBytes:    limit.MonthlyLimitBytes,
		HardLimitEnabled:     limit.HardLimitEnabled,
		SpeedLimitBps:        limit.SpeedLimitBps,
		ResetDay:             limit.ResetDay,
		LimitExceededAt:      limit.LimitExceededAt,
		EnforcementStatus:    limit.EnforcementStatus,
		EnforcementUpdatedAt: limit.EnforcementUpdatedAt,
		UpdatedAt:            limit.UpdatedAt,
	}
	if state.EnforcementStatus == "" {
		state.EnforcementStatus = TrafficLimitEnforcementNotEnforced
	}
	state.Enforced = state.EnforcementStatus == TrafficLimitEnforcementOverLimit

	if limit.MonthlyLimitBytes != nil && *limit.MonthlyLimitBytes > 0 {
		usedPercent := float64(totalBytes) / float64(*limit.MonthlyLimitBytes) * 100
		state.UsedPercent = &usedPercent
		state.LimitReached = totalBytes >= *limit.MonthlyLimitBytes

		remainingBytes := *limit.MonthlyLimitBytes - totalBytes
		if remainingBytes < 0 {
			remainingBytes = 0
		}
		state.RemainingBytes = &remainingBytes
	}
	return state
}
