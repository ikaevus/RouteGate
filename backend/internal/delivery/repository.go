package delivery

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const deliveryColumns = `
	id::text,
	COALESCE(vpn_account_id::text, ''),
	channel,
	provider,
	recipient,
	template_key,
	locale,
	attach_qr,
	status,
	attempt_count,
	max_attempts,
	next_attempt_at,
	attempt_started_at,
	COALESCE(provider_reference, ''),
	COALESCE(last_error_class, ''),
	COALESCE(last_error_code, ''),
	COALESCE(idempotency_key, ''),
	COALESCE(created_by_user_id::text, ''),
	created_at,
	updated_at,
	sent_at,
	completed_at`

const deliveryColumnsD = `
	d.id::text,
	COALESCE(d.vpn_account_id::text, ''),
	d.channel,
	d.provider,
	d.recipient,
	d.template_key,
	d.locale,
	d.attach_qr,
	d.status,
	d.attempt_count,
	d.max_attempts,
	d.next_attempt_at,
	d.attempt_started_at,
	COALESCE(d.provider_reference, ''),
	COALESCE(d.last_error_class, ''),
	COALESCE(d.last_error_code, ''),
	COALESCE(d.idempotency_key, ''),
	COALESCE(d.created_by_user_id::text, ''),
	d.created_at,
	d.updated_at,
	d.sent_at,
	d.completed_at`

type scanner interface {
	Scan(dest ...any) error
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, input CreateInput) (Delivery, bool, error) {
	input = normalizeCreateInput(input)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Delivery{}, false, err
	}
	defer tx.Rollback(ctx)

	delivery, err := scanDelivery(tx.QueryRow(ctx, `
		INSERT INTO deliveries (
			vpn_account_id,
			channel,
			provider,
			recipient,
			template_key,
			locale,
			attach_qr,
			max_attempts,
			idempotency_key,
			created_by_user_id
		)
		VALUES (
			NULLIF($1, '')::uuid,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			NULLIF($9, ''),
			NULLIF($10, '')::uuid
		)
		ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING `+deliveryColumns+`
	`,
		input.VPNAccountID,
		input.Channel,
		input.Provider,
		input.Recipient,
		input.TemplateKey,
		input.Locale,
		input.AttachQR,
		input.MaxAttempts,
		input.IdempotencyKey,
		input.CreatedByUserID,
	))
	created := true
	if err == pgx.ErrNoRows && input.IdempotencyKey != "" {
		delivery, err = scanDelivery(tx.QueryRow(ctx, `
			SELECT `+deliveryColumns+`
			FROM deliveries
			WHERE idempotency_key = $1
		`, input.IdempotencyKey))
		created = false
	}
	if err != nil {
		return Delivery{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Delivery{}, false, err
	}
	return delivery, created, nil
}

func (r *Repository) ClaimNext(ctx context.Context) (*Delivery, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	delivery, err := scanDelivery(tx.QueryRow(ctx, `
		WITH next_delivery AS (
			SELECT id
			FROM deliveries
			WHERE status IN ('queued', 'retrying')
			  AND attempt_count < max_attempts
			  AND COALESCE(next_attempt_at, now()) <= now()
			ORDER BY next_attempt_at NULLS FIRST, created_at, id
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE deliveries d
		SET
			status = 'sending',
			attempt_count = d.attempt_count + 1,
			attempt_started_at = now(),
			next_attempt_at = NULL,
			last_error_class = NULL,
			last_error_code = NULL,
			updated_at = now()
		FROM next_delivery
		WHERE d.id = next_delivery.id
		RETURNING `+deliveryColumnsD+`
	`))
	if err == pgx.ErrNoRows {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, commitErr
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &delivery, nil
}

func (r *Repository) MarkSent(ctx context.Context, id, providerReference string) (Delivery, error) {
	return scanDelivery(r.pool.QueryRow(ctx, `
		UPDATE deliveries
		SET
			status = 'sent',
			provider_reference = NULLIF($2, ''),
			sent_at = COALESCE(sent_at, now()),
			completed_at = now(),
			updated_at = now()
		WHERE id = $1::uuid
		  AND status = 'sending'
		RETURNING `+deliveryColumns+`
	`, id, strings.TrimSpace(providerReference)))
}

func (r *Repository) MarkDelivered(ctx context.Context, id, providerReference string) (Delivery, error) {
	return scanDelivery(r.pool.QueryRow(ctx, `
		UPDATE deliveries
		SET
			status = 'delivered',
			provider_reference = COALESCE(NULLIF($2, ''), provider_reference),
			sent_at = COALESCE(sent_at, now()),
			completed_at = now(),
			updated_at = now()
		WHERE id = $1::uuid
		  AND status IN ('sending', 'sent')
		RETURNING `+deliveryColumns+`
	`, id, strings.TrimSpace(providerReference)))
}

func (r *Repository) MarkRetrying(ctx context.Context, id string, nextAttemptAt time.Time, class ErrorClass, code string) (Delivery, error) {
	return scanDelivery(r.pool.QueryRow(ctx, `
		UPDATE deliveries
		SET
			status = 'retrying',
			next_attempt_at = $2,
			attempt_started_at = NULL,
			last_error_class = NULLIF($3, ''),
			last_error_code = NULLIF($4, ''),
			completed_at = NULL,
			updated_at = now()
		WHERE id = $1::uuid
		  AND status = 'sending'
		RETURNING `+deliveryColumns+`
	`, id, nextAttemptAt.UTC(), string(class), normalizeSafeCode(code)))
}

func (r *Repository) MarkFailed(ctx context.Context, id string, class ErrorClass, code string) (Delivery, error) {
	return r.markTerminal(ctx, id, StatusFailed, class, code)
}

func (r *Repository) MarkUncertain(ctx context.Context, id string, code string) (Delivery, error) {
	return r.markTerminal(ctx, id, StatusUncertain, ErrorClassUncertain, code)
}

func (r *Repository) markTerminal(ctx context.Context, id string, status Status, class ErrorClass, code string) (Delivery, error) {
	return scanDelivery(r.pool.QueryRow(ctx, `
		UPDATE deliveries
		SET
			status = $2,
			next_attempt_at = NULL,
			last_error_class = NULLIF($3, ''),
			last_error_code = NULLIF($4, ''),
			completed_at = now(),
			updated_at = now()
		WHERE id = $1::uuid
		  AND status = 'sending'
		RETURNING `+deliveryColumns+`
	`, id, string(status), string(class), normalizeSafeCode(code)))
}

func (r *Repository) RecoverSendingAfterRestart(ctx context.Context) ([]Delivery, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE deliveries
		SET
			status = 'uncertain',
			next_attempt_at = NULL,
			last_error_class = 'uncertain',
			last_error_code = 'manager_restart',
			completed_at = now(),
			updated_at = now()
		WHERE status = 'sending'
		RETURNING `+deliveryColumns+`
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	deliveries := make([]Delivery, 0)
	for rows.Next() {
		delivery, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return deliveries, nil
}

func normalizeCreateInput(input CreateInput) CreateInput {
	input.VPNAccountID = strings.TrimSpace(input.VPNAccountID)
	input.Channel = strings.TrimSpace(input.Channel)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Recipient = strings.TrimSpace(input.Recipient)
	input.TemplateKey = strings.TrimSpace(input.TemplateKey)
	input.Locale = strings.ToLower(strings.TrimSpace(input.Locale))
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.CreatedByUserID = strings.TrimSpace(input.CreatedByUserID)
	if input.Locale == "" {
		input.Locale = "en"
	}
	if input.MaxAttempts == 0 {
		input.MaxAttempts = 5
	}
	return input
}

func scanDelivery(row scanner) (Delivery, error) {
	var delivery Delivery
	var status string
	var nextAttemptAt sql.NullTime
	var attemptStartedAt sql.NullTime
	var sentAt sql.NullTime
	var completedAt sql.NullTime
	var lastErrorClass string

	if err := row.Scan(
		&delivery.ID,
		&delivery.VPNAccountID,
		&delivery.Channel,
		&delivery.Provider,
		&delivery.Recipient,
		&delivery.TemplateKey,
		&delivery.Locale,
		&delivery.AttachQR,
		&status,
		&delivery.AttemptCount,
		&delivery.MaxAttempts,
		&nextAttemptAt,
		&attemptStartedAt,
		&delivery.ProviderReference,
		&lastErrorClass,
		&delivery.LastErrorCode,
		&delivery.IdempotencyKey,
		&delivery.CreatedByUserID,
		&delivery.CreatedAt,
		&delivery.UpdatedAt,
		&sentAt,
		&completedAt,
	); err != nil {
		return Delivery{}, err
	}

	delivery.Status = Status(status)
	delivery.LastErrorClass = ErrorClass(lastErrorClass)
	if nextAttemptAt.Valid {
		value := nextAttemptAt.Time.UTC()
		delivery.NextAttemptAt = &value
	}
	if attemptStartedAt.Valid {
		value := attemptStartedAt.Time.UTC()
		delivery.AttemptStartedAt = &value
	}
	if sentAt.Valid {
		value := sentAt.Time.UTC()
		delivery.SentAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		delivery.CompletedAt = &value
	}
	delivery.CreatedAt = delivery.CreatedAt.UTC()
	delivery.UpdatedAt = delivery.UpdatedAt.UTC()
	return delivery, nil
}
