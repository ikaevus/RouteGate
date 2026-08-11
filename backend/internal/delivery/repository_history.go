package delivery

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) Get(ctx context.Context, id string) (Delivery, error) {
	return scanDelivery(r.pool.QueryRow(ctx, `
		SELECT `+deliveryColumns+`
		FROM deliveries
		WHERE id = $1::uuid
	`, id))
}

func (r *Repository) ListForVPNAccount(ctx context.Context, vpnAccountID string, limit int) ([]Delivery, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+deliveryColumns+`
		FROM deliveries
		WHERE vpn_account_id = $1::uuid
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, vpnAccountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Delivery, 0)
	for rows.Next() {
		item, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) Requeue(ctx context.Context, id string) (Delivery, error) {
	return scanDelivery(r.pool.QueryRow(ctx, `
		UPDATE deliveries
		SET
			status = 'queued',
			max_attempts = GREATEST(max_attempts, attempt_count + 1),
			next_attempt_at = now(),
			attempt_started_at = NULL,
			provider_reference = NULL,
			last_error_class = NULL,
			last_error_code = NULL,
			completed_at = NULL,
			updated_at = now()
		WHERE id = $1::uuid
		  AND status IN ('failed', 'uncertain')
		RETURNING `+deliveryColumns+`
	`, id))
}

func IsNotFound(err error) bool {
	return err == pgx.ErrNoRows
}
