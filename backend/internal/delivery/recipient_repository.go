package delivery

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DeliveryRecipient struct {
	Channel   string
	Provider  string
	Recipient string
	Locale    string
}

type RecipientRepository struct{ pool *pgxpool.Pool }

func NewRecipientRepository(pool *pgxpool.Pool) *RecipientRepository {
	return &RecipientRepository{pool: pool}
}

func (r *RecipientRepository) ListEnabled(ctx context.Context) ([]DeliveryRecipient, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT channel, provider, address,
		       CASE WHEN lower(COALESCE(metadata_json->>'locale',''))='ru' THEN 'ru' ELSE 'en' END
		FROM delivery_recipients
		WHERE enabled=TRUE
		ORDER BY provider, address
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]DeliveryRecipient, 0)
	for rows.Next() {
		var item DeliveryRecipient
		if err := rows.Scan(&item.Channel, &item.Provider, &item.Recipient, &item.Locale); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
