package portal

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) ListProfilesForUser(ctx context.Context, email string) ([]PortalProfile, error) {
	rows, err := r.pool.Query(ctx, portalProfileSelect+`
		WHERE lower(COALESCE(a.email, '')) = lower($1)
		ORDER BY a.created_at DESC
	`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PortalProfile, 0)
	for rows.Next() {
		item, err := scanPortalProfile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetProfileForUser(ctx context.Context, email string, profileID string) (PortalProfile, error) {
	return scanPortalProfile(r.pool.QueryRow(ctx, portalProfileSelect+`
		WHERE a.id = $1::uuid
		  AND lower(COALESCE(a.email, '')) = lower($2)
	`, profileID, email))
}

func (r *Repository) GetTrafficUsageForUser(ctx context.Context, email string) (TrafficUsageSummary, error) {
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	usage := TrafficUsageSummary{
		Enabled:     true,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	}
	var lastObservedAt sql.NullTime

	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(e.rx_bytes), 0)::bigint,
			COALESCE(SUM(e.tx_bytes), 0)::bigint,
			MAX(e.observed_at)
		FROM traffic_usage_events e
		JOIN vpn_accounts a ON a.id = e.vpn_account_id
		WHERE lower(COALESCE(a.email, '')) = lower($1)
		  AND e.observed_at >= $2
		  AND e.observed_at < $3
	`, email, periodStart, periodEnd).Scan(
		&usage.RXBytes,
		&usage.TXBytes,
		&lastObservedAt,
	)
	if err != nil {
		return TrafficUsageSummary{}, err
	}

	usage.TotalBytes = usage.RXBytes + usage.TXBytes
	if lastObservedAt.Valid {
		value := lastObservedAt.Time.UTC()
		usage.LastObservedAt = &value
	}
	return usage, nil
}

func (r *Repository) CreateSubscriptionToken(ctx context.Context, input CreateSubscriptionTokenInput) (PortalSubscriptionToken, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PortalSubscriptionToken{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE vpn_subscription_tokens
		SET status = 'revoked', revoked_at = now(), updated_at = now()
		WHERE vpn_account_id = $1::uuid AND status = 'active'
	`, input.VPNAccountID); err != nil {
		return PortalSubscriptionToken{}, err
	}

	token, err := scanPortalSubscriptionToken(tx.QueryRow(ctx, `
		INSERT INTO vpn_subscription_tokens (vpn_account_id, token_hash, expires_at)
		VALUES ($1::uuid, $2, $3)
		RETURNING
			id::text,
			vpn_account_id::text,
			token_hash,
			status,
			expires_at,
			created_at,
			updated_at
	`, input.VPNAccountID, input.TokenHash, input.ExpiresAt))
	if err != nil {
		return PortalSubscriptionToken{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PortalSubscriptionToken{}, err
	}
	return token, nil
}

const portalProfileSelect = `
	SELECT
		a.id::text,
		a.display_name,
		a.status,
		a.expires_at,
		a.max_devices,
		COALESCE(a.protocol, 'sing-box'),
		COALESCE(s.location, ''),
		a.updated_at
	FROM vpn_accounts a
	LEFT JOIN servers s ON s.id = a.server_id
`

type scanner interface {
	Scan(dest ...any) error
}

func scanPortalProfile(row scanner) (PortalProfile, error) {
	var profile PortalProfile
	var expiresAt sql.NullTime
	var maxDevices sql.NullInt32

	err := row.Scan(
		&profile.ID,
		&profile.DisplayName,
		&profile.Status,
		&expiresAt,
		&maxDevices,
		&profile.Protocol,
		&profile.Location,
		&profile.UpdatedAt,
	)
	if err != nil {
		return PortalProfile{}, err
	}
	if expiresAt.Valid {
		profile.ExpiresAt = &expiresAt.Time
	}
	if maxDevices.Valid {
		value := int(maxDevices.Int32)
		profile.MaxDevices = &value
	}
	profile.AccessStatus = accessStatus(profile, time.Now().UTC())
	return profile, nil
}

func scanPortalSubscriptionToken(row scanner) (PortalSubscriptionToken, error) {
	var token PortalSubscriptionToken
	var expiresAt sql.NullTime
	err := row.Scan(
		&token.ID,
		&token.VPNAccountID,
		&token.TokenHash,
		&token.Status,
		&expiresAt,
		&token.CreatedAt,
		&token.UpdatedAt,
	)
	if err != nil {
		return PortalSubscriptionToken{}, err
	}
	if expiresAt.Valid {
		token.ExpiresAt = &expiresAt.Time
	}
	return token, nil
}

func accessStatus(profile PortalProfile, now time.Time) string {
	if profile.ExpiresAt != nil && !profile.ExpiresAt.After(now) {
		return AccessStatusExpired
	}
	switch profile.Status {
	case "active":
		return AccessStatusActive
	case "suspended", "revoked":
		return AccessStatusSuspended
	case "expired":
		return AccessStatusExpired
	case "created":
		return AccessStatusPending
	default:
		return AccessStatusNoAccess
	}
}
