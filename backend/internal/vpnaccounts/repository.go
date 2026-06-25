package vpnaccounts

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateAccount(ctx context.Context, input CreateAccountInput) (Account, error) {
	status := input.Status
	if status == "" {
		status = StatusCreated
	}

	return scanAccount(r.pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (
			username, protocol, display_name, email, status, expires_at, max_devices, server_id
		)
		VALUES (
			$1, 'sing-box', $1, NULLIF($2, ''), $3, $4, $5, NULLIF($6, '')::uuid
		)
		RETURNING
			id::text,
			display_name,
			COALESCE(email, ''),
			status,
			expires_at,
			max_devices,
			COALESCE(server_id::text, ''),
			created_at,
			updated_at
	`, input.DisplayName, input.Email, status, input.ExpiresAt, input.MaxDevices, input.ServerID))
}

func (r *Repository) ListAccounts(ctx context.Context, filter AccountFilter) ([]Account, error) {
	rows, err := r.pool.Query(ctx, accountSelect+`
		WHERE ($1 = '' OR status = $1)
		  AND ($2 = '' OR server_id = $2::uuid)
		  AND ($3 = '' OR display_name ILIKE '%' || $3 || '%' OR COALESCE(email, '') ILIKE '%' || $3 || '%')
		ORDER BY created_at DESC
		LIMIT CASE WHEN $4 > 0 THEN $4 ELSE 100 END
		OFFSET CASE WHEN $5 > 0 THEN $5 ELSE 0 END
	`, filter.Status, filter.ServerID, filter.Search, filter.Limit, filter.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Account, 0)
	for rows.Next() {
		item, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetAccountByID(ctx context.Context, id string) (Account, error) {
	return scanAccount(r.pool.QueryRow(ctx, accountSelect+` WHERE id = $1::uuid`, id))
}

func (r *Repository) UpdateAccount(ctx context.Context, id string, input UpdateAccountInput) (Account, error) {
	return scanAccount(r.pool.QueryRow(ctx, `
		UPDATE vpn_accounts
		SET
			username = CASE WHEN $2 THEN $3 ELSE username END,
			display_name = CASE WHEN $2 THEN $3 ELSE display_name END,
			email = CASE WHEN $4 THEN NULLIF($5, '') ELSE email END,
			status = CASE WHEN $6 THEN $7 ELSE status END,
			expires_at = CASE WHEN $8 THEN $9 ELSE expires_at END,
			max_devices = CASE WHEN $10 THEN $11 ELSE max_devices END,
			server_id = CASE WHEN $12 THEN NULLIF($13, '')::uuid ELSE server_id END,
			updated_at = now()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			display_name,
			COALESCE(email, ''),
			status,
			expires_at,
			max_devices,
			COALESCE(server_id::text, ''),
			created_at,
			updated_at
	`,
		id,
		input.DisplayName != nil, stringValue(input.DisplayName),
		input.Email != nil, stringValue(input.Email),
		input.Status != nil, stringValue(input.Status),
		input.ExpiresAt != nil, input.ExpiresAt,
		input.MaxDevices != nil, input.MaxDevices,
		input.ServerID != nil, stringValue(input.ServerID),
	))
}

func (r *Repository) SetAccountStatus(ctx context.Context, id string, status string) (Account, error) {
	return scanAccount(r.pool.QueryRow(ctx, `
		UPDATE vpn_accounts
		SET status = $2, updated_at = now()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			display_name,
			COALESCE(email, ''),
			status,
			expires_at,
			max_devices,
			COALESCE(server_id::text, ''),
			created_at,
			updated_at
	`, id, status))
}

func (r *Repository) DeleteAccount(ctx context.Context, id string) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM vpn_accounts WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) CreateSubscriptionToken(ctx context.Context, input CreateSubscriptionTokenInput) (SubscriptionToken, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SubscriptionToken{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE vpn_subscription_tokens
		SET status = 'revoked', revoked_at = now(), updated_at = now()
		WHERE vpn_account_id = $1::uuid AND status = 'active'
	`, input.VPNAccountID); err != nil {
		return SubscriptionToken{}, err
	}

	token, err := scanSubscriptionToken(tx.QueryRow(ctx, `
		INSERT INTO vpn_subscription_tokens (vpn_account_id, token_hash, expires_at)
		VALUES ($1::uuid, $2, $3)
		RETURNING
			id::text,
			vpn_account_id::text,
			token_hash,
			status,
			expires_at,
			last_used_at,
			revoked_at,
			created_at,
			updated_at
	`, input.VPNAccountID, input.TokenHash, input.ExpiresAt))
	if err != nil {
		return SubscriptionToken{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SubscriptionToken{}, err
	}
	return token, nil
}

func (r *Repository) RevokeActiveSubscriptionTokens(ctx context.Context, vpnAccountID string) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE vpn_subscription_tokens
		SET status = 'revoked', revoked_at = now(), updated_at = now()
		WHERE vpn_account_id = $1::uuid AND status = 'active'
	`, vpnAccountID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) GetActiveSubscriptionTokenByHash(ctx context.Context, vpnAccountID string, tokenHash string) (SubscriptionToken, error) {
	return scanSubscriptionToken(r.pool.QueryRow(ctx, subscriptionTokenSelect+`
		WHERE vpn_account_id = $1::uuid
		  AND token_hash = $2
		  AND status = 'active'
		  AND (expires_at IS NULL OR expires_at > now())
	`, vpnAccountID, tokenHash))
}

func (r *Repository) FindActiveSubscriptionTokenByHash(ctx context.Context, tokenHash string) (SubscriptionToken, error) {
	return scanSubscriptionToken(r.pool.QueryRow(ctx, subscriptionTokenSelect+`
		WHERE token_hash = $1
		  AND status = 'active'
		  AND (expires_at IS NULL OR expires_at > now())
	`, tokenHash))
}

func (r *Repository) MarkSubscriptionTokenUsed(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE vpn_subscription_tokens
		SET last_used_at = now(), updated_at = now()
		WHERE id = $1::uuid
	`, id)
	return err
}

const accountSelect = `
	SELECT
		id::text,
		display_name,
		COALESCE(email, ''),
		status,
		expires_at,
		max_devices,
		COALESCE(server_id::text, ''),
		created_at,
		updated_at
	FROM vpn_accounts`

const subscriptionTokenSelect = `
	SELECT
		id::text,
		vpn_account_id::text,
		token_hash,
		status,
		expires_at,
		last_used_at,
		revoked_at,
		created_at,
		updated_at
	FROM vpn_subscription_tokens`

type scanner interface {
	Scan(dest ...any) error
}

func scanAccount(row scanner) (Account, error) {
	var account Account
	var expiresAt sql.NullTime
	var maxDevices sql.NullInt32
	err := row.Scan(
		&account.ID,
		&account.DisplayName,
		&account.Email,
		&account.Status,
		&expiresAt,
		&maxDevices,
		&account.ServerID,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	if err != nil {
		return Account{}, err
	}
	if expiresAt.Valid {
		account.ExpiresAt = &expiresAt.Time
	}
	if maxDevices.Valid {
		value := int(maxDevices.Int32)
		account.MaxDevices = &value
	}
	return account, nil
}

func scanSubscriptionToken(row scanner) (SubscriptionToken, error) {
	var token SubscriptionToken
	var expiresAt, lastUsedAt, revokedAt sql.NullTime
	err := row.Scan(
		&token.ID,
		&token.VPNAccountID,
		&token.TokenHash,
		&token.Status,
		&expiresAt,
		&lastUsedAt,
		&revokedAt,
		&token.CreatedAt,
		&token.UpdatedAt,
	)
	if err != nil {
		return SubscriptionToken{}, err
	}
	if expiresAt.Valid {
		token.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		token.LastUsedAt = &lastUsedAt.Time
	}
	if revokedAt.Valid {
		token.RevokedAt = &revokedAt.Time
	}
	return token, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
