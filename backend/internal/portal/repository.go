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
