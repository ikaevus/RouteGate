package observability

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const notificationClaimTimeout = time.Minute

type NotificationIntent struct {
	ID         string
	AlertID    string
	Kind       string
	Severity   Severity
	RuleKey    string
	Resource   ResourceRef
	ReasonCode string
	Summary    string
}

type NotificationRecipient struct {
	ID       string
	Channel  string
	Provider string
	Address  string
	Locale   string
}

type NotificationRepository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewNotificationRepository(pool *pgxpool.Pool) *NotificationRepository {
	return &NotificationRepository{pool: pool, now: time.Now}
}

func (r *NotificationRepository) ClaimNext(ctx context.Context) (*NotificationIntent, error) {
	now := r.now().UTC()
	var intent NotificationIntent
	var severity string
	err := r.pool.QueryRow(ctx, `
		WITH next_intent AS (
			SELECT id
			FROM observability_notification_intents
			WHERE expanded_at IS NULL
			  AND (claimed_at IS NULL OR claimed_at <= $1)
			ORDER BY created_at, id
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE observability_notification_intents i
		SET claimed_at=$2, updated_at=now()
		FROM next_intent
		WHERE i.id=next_intent.id
		RETURNING i.id::text, i.alert_id::text, i.kind, i.severity, i.rule_key,
		          i.resource_type, i.resource_id, COALESCE(i.reason_code,''), i.summary
	`, now.Add(-notificationClaimTimeout), now).Scan(
		&intent.ID, &intent.AlertID, &intent.Kind, &severity, &intent.RuleKey,
		&intent.Resource.Type, &intent.Resource.ID, &intent.ReasonCode, &intent.Summary,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	intent.Severity = Severity(severity)
	return &intent, nil
}

func (r *NotificationRepository) ListEnabledRecipients(ctx context.Context) ([]NotificationRecipient, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, channel, provider, address, metadata_json
		FROM delivery_recipients
		WHERE enabled=TRUE AND provider IN ('smtp','telegram')
		ORDER BY provider, created_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]NotificationRecipient, 0)
	for rows.Next() {
		var item NotificationRecipient
		var metadata []byte
		if err := rows.Scan(&item.ID, &item.Channel, &item.Provider, &item.Address, &metadata); err != nil {
			return nil, err
		}
		item.Locale = notificationRecipientLocale(metadata)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *NotificationRepository) LinkDelivery(ctx context.Context, intentID, recipientID, deliveryID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO observability_notification_deliveries (delivery_id, intent_id, recipient_id)
		VALUES ($1::uuid,$2::uuid,$3::uuid)
		ON CONFLICT (delivery_id) DO NOTHING
	`, deliveryID, intentID, recipientID)
	return err
}

func (r *NotificationRepository) MarkExpanded(ctx context.Context, intentID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE observability_notification_intents
		SET expanded_at=$2, claimed_at=NULL, updated_at=now()
		WHERE id=$1::uuid AND expanded_at IS NULL
	`, intentID, r.now().UTC())
	return err
}

func notificationRecipientLocale(raw []byte) string {
	var metadata struct {
		Locale string `json:"locale"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &metadata)
	}
	if strings.EqualFold(strings.TrimSpace(metadata.Locale), "ru") {
		return "ru"
	}
	return "en"
}
