package delivery

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const telegramPairingTTL = 10 * time.Minute

type DeliveryRecipientResponse struct {
	ID          string    `json:"id"`
	Channel     string    `json:"channel"`
	Provider    string    `json:"provider"`
	Recipient   string    `json:"recipient"`
	DisplayName string    `json:"displayName"`
	Username    string    `json:"username,omitempty"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type DeliveryRecipientListResponse struct {
	Items []DeliveryRecipientResponse `json:"items"`
}

type TelegramPairingResponse struct {
	ID          string                     `json:"id"`
	State       string                     `json:"state"`
	BotUsername string                     `json:"botUsername"`
	DeepLink    string                     `json:"deepLink,omitempty"`
	ExpiresAt   time.Time                  `json:"expiresAt"`
	Recipient   *DeliveryRecipientResponse `json:"recipient,omitempty"`
	ErrorCode   string                     `json:"errorCode,omitempty"`
}

type TelegramRecipientTestResponse struct {
	OK        bool   `json:"ok"`
	ErrorCode string `json:"errorCode,omitempty"`
}

type TelegramPairingManager struct {
	pool     *pgxpool.Pool
	settings *ProviderSettingsManager
}

func NewTelegramPairingManager(pool *pgxpool.Pool, settings *ProviderSettingsManager) *TelegramPairingManager {
	return &TelegramPairingManager{pool: pool, settings: settings}
}

func (m *TelegramPairingManager) Start(ctx context.Context, createdBy string) (TelegramPairingResponse, error) {
	provider, code, err := m.provider(ctx)
	if err != nil {
		return TelegramPairingResponse{}, err
	}
	if code != "" {
		return TelegramPairingResponse{}, Failure{Class: ErrorClassPermanent, Code: code}
	}
	identity, code := provider.BotIdentity(ctx)
	if code != "" {
		return TelegramPairingResponse{}, Failure{Class: ErrorClassPermanent, Code: code}
	}

	parameter, err := generateTelegramStartParameter()
	if err != nil {
		return TelegramPairingResponse{}, err
	}
	hash := sha256.Sum256([]byte(parameter))
	expiresAt := time.Now().UTC().Add(telegramPairingTTL)
	var sessionID string
	var userID any
	if strings.TrimSpace(createdBy) != "" {
		userID = createdBy
	}
	if err := m.pool.QueryRow(ctx, `
		INSERT INTO telegram_pairing_sessions (
			start_parameter_hash, bot_username, created_by_user_id, expires_at
		) VALUES ($1, $2, $3::uuid, $4)
		RETURNING id::text
	`, hash[:], identity.Username, userID, expiresAt).Scan(&sessionID); err != nil {
		return TelegramPairingResponse{}, err
	}
	_, _ = m.pool.Exec(ctx, `DELETE FROM telegram_pairing_sessions WHERE expires_at < NOW() - interval '1 day'`)

	return TelegramPairingResponse{
		ID:          sessionID,
		State:       "pending",
		BotUsername: identity.Username,
		DeepLink:    "https://t.me/" + identity.Username + "?start=" + url.QueryEscape(parameter),
		ExpiresAt:   expiresAt,
	}, nil
}

func (m *TelegramPairingManager) Get(ctx context.Context, sessionID string) (TelegramPairingResponse, error) {
	if err := m.processUpdates(ctx); err != nil {
		var failure Failure
		if errors.As(err, &failure) {
			view, viewErr := m.loadSession(ctx, sessionID)
			if viewErr == nil {
				view.ErrorCode = failure.Code
				return view, nil
			}
		}
		return TelegramPairingResponse{}, err
	}
	return m.loadSession(ctx, sessionID)
}

func (m *TelegramPairingManager) ListRecipients(ctx context.Context) ([]DeliveryRecipientResponse, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT id::text, channel, provider, address, display_name, username, enabled, created_at, updated_at
		FROM delivery_recipients
		WHERE provider='telegram'
		ORDER BY enabled DESC, lower(display_name), created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]DeliveryRecipientResponse, 0)
	for rows.Next() {
		var item DeliveryRecipientResponse
		if err := rows.Scan(&item.ID, &item.Channel, &item.Provider, &item.Recipient, &item.DisplayName, &item.Username, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (m *TelegramPairingManager) DeleteRecipient(ctx context.Context, recipientID string) (bool, error) {
	command, err := m.pool.Exec(ctx, `DELETE FROM delivery_recipients WHERE id=$1::uuid AND provider='telegram'`, recipientID)
	if err != nil {
		return false, err
	}
	return command.RowsAffected() > 0, nil
}

func (m *TelegramPairingManager) TestRecipient(ctx context.Context, recipientID string) TelegramRecipientTestResponse {
	provider, code, err := m.provider(ctx)
	if err != nil {
		return TelegramRecipientTestResponse{OK: false, ErrorCode: "telegram_pairing_unavailable"}
	}
	if code != "" {
		return TelegramRecipientTestResponse{OK: false, ErrorCode: code}
	}
	var recipient string
	if err := m.pool.QueryRow(ctx, `
		SELECT address FROM delivery_recipients
		WHERE id=$1::uuid AND provider='telegram' AND enabled=TRUE
	`, recipientID).Scan(&recipient); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TelegramRecipientTestResponse{OK: false, ErrorCode: "delivery_recipient_not_found"}
		}
		return TelegramRecipientTestResponse{OK: false, ErrorCode: "telegram_pairing_unavailable"}
	}
	result := provider.Send(ctx, Message{
		Recipient: recipient,
		Text:      "RouteGate Telegram test: notifications and delivery are connected.",
	})
	return TelegramRecipientTestResponse{
		OK:        result.Outcome == OutcomeAccepted || result.Outcome == OutcomeDelivered,
		ErrorCode: result.ErrorCode,
	}
}

func (m *TelegramPairingManager) provider(ctx context.Context) (*TelegramProvider, string, error) {
	provider, found, err := m.settings.Resolve(ctx, "telegram")
	if err != nil {
		return nil, "", err
	}
	if !found || provider == nil {
		return nil, "telegram_not_configured", nil
	}
	telegramProvider, ok := provider.(*TelegramProvider)
	if !ok || !telegramProvider.Configured() {
		return nil, "telegram_not_configured", nil
	}
	return telegramProvider, "", nil
}

func (m *TelegramPairingManager) loadSession(ctx context.Context, sessionID string) (TelegramPairingResponse, error) {
	var view TelegramPairingResponse
	var recipientID *string
	err := m.pool.QueryRow(ctx, `
		SELECT id::text, bot_username, expires_at, recipient_id::text
		FROM telegram_pairing_sessions
		WHERE id=$1::uuid
	`, sessionID).Scan(&view.ID, &view.BotUsername, &view.ExpiresAt, &recipientID)
	if err != nil {
		return TelegramPairingResponse{}, err
	}
	if recipientID != nil && strings.TrimSpace(*recipientID) != "" {
		recipient, err := m.getRecipient(ctx, *recipientID)
		if err != nil {
			return TelegramPairingResponse{}, err
		}
		view.State = "paired"
		view.Recipient = &recipient
		return view, nil
	}
	if time.Now().UTC().After(view.ExpiresAt) {
		view.State = "expired"
		return view, nil
	}
	view.State = "pending"
	return view, nil
}

func (m *TelegramPairingManager) getRecipient(ctx context.Context, recipientID string) (DeliveryRecipientResponse, error) {
	var item DeliveryRecipientResponse
	err := m.pool.QueryRow(ctx, `
		SELECT id::text, channel, provider, address, display_name, username, enabled, created_at, updated_at
		FROM delivery_recipients
		WHERE id=$1::uuid AND provider='telegram'
	`, recipientID).Scan(&item.ID, &item.Channel, &item.Provider, &item.Recipient, &item.DisplayName, &item.Username, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (m *TelegramPairingManager) processUpdates(ctx context.Context) error {
	provider, code, err := m.provider(ctx)
	if err != nil {
		return err
	}
	if code != "" {
		return Failure{Class: ErrorClassPermanent, Code: code}
	}

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var pending int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM telegram_pairing_sessions WHERE recipient_id IS NULL AND expires_at > NOW()`).Scan(&pending); err != nil {
		return err
	}
	if pending == 0 {
		return tx.Commit(ctx)
	}

	var offset int64
	if err := tx.QueryRow(ctx, `SELECT next_update_id FROM telegram_update_state WHERE provider='telegram' FOR UPDATE`).Scan(&offset); err != nil {
		return err
	}
	updates, nextOffset, code := provider.GetUpdates(ctx, offset)
	if code != "" {
		return Failure{Class: ErrorClassPermanent, Code: code}
	}

	for _, update := range updates {
		if update.ChatType != "private" || update.ChatID == 0 {
			continue
		}
		parameter, ok := telegramStartParameter(update.Text)
		if !ok {
			continue
		}
		hash := sha256.Sum256([]byte(parameter))
		var sessionID string
		var createdBy *string
		err := tx.QueryRow(ctx, `
			SELECT id::text, created_by_user_id::text
			FROM telegram_pairing_sessions
			WHERE start_parameter_hash=$1 AND recipient_id IS NULL AND expires_at > NOW()
			FOR UPDATE
		`, hash[:]).Scan(&sessionID, &createdBy)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}

		address := strconv.FormatInt(update.ChatID, 10)
		var recipientID string
		var userID any
		if createdBy != nil && strings.TrimSpace(*createdBy) != "" {
			userID = *createdBy
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO delivery_recipients (
				channel, provider, address, display_name, username, enabled, created_by_user_id
			) VALUES ('telegram', 'telegram', $1, $2, $3, TRUE, $4::uuid)
			ON CONFLICT (provider, address) DO UPDATE SET
				display_name=EXCLUDED.display_name,
				username=EXCLUDED.username,
				enabled=TRUE,
				updated_at=NOW()
			RETURNING id::text
		`, address, telegramDisplayName(update), update.Username, userID).Scan(&recipientID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE telegram_pairing_sessions
			SET recipient_id=$2::uuid, consumed_at=NOW()
			WHERE id=$1::uuid AND recipient_id IS NULL
		`, sessionID, recipientID); err != nil {
			return err
		}
	}
	if nextOffset > offset {
		if _, err := tx.Exec(ctx, `
			UPDATE telegram_update_state SET next_update_id=$1, updated_at=NOW() WHERE provider='telegram'
		`, nextOffset); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func generateTelegramStartParameter() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func telegramStartParameter(text string) (string, bool) {
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) != 2 {
		return "", false
	}
	command := parts[0]
	if command != "/start" && !strings.HasPrefix(command, "/start@") {
		return "", false
	}
	parameter := strings.TrimSpace(parts[1])
	if parameter == "" || len(parameter) > 64 || strings.ContainsAny(parameter, " \t\r\n") {
		return "", false
	}
	return parameter, true
}

func telegramDisplayName(update TelegramIncomingUpdate) string {
	name := strings.TrimSpace(strings.Join([]string{update.FirstName, update.LastName}, " "))
	if name != "" {
		return name
	}
	if update.Username != "" {
		return "@" + update.Username
	}
	return "Telegram"
}
