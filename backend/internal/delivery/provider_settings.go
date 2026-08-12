package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/config"
	"github.com/ikaevus/routegate/backend/internal/secrets"
)

const (
	providerSourceManaged     = "managed"
	providerSourceEnvironment = "environment"
	providerSourceNone        = "none"
)

var canonicalProviderNames = []string{"smtp", "telegram"}

type ProviderSettingsRecord struct {
	Provider         string
	Enabled          bool
	ConfigJSON       []byte
	SecretCiphertext []byte
	SecretNonce      []byte
	SecretKeyVersion int
	UpdatedByUserID  string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ProviderSettingsRepository struct {
	pool *pgxpool.Pool
}

func NewProviderSettingsRepository(pool *pgxpool.Pool) *ProviderSettingsRepository {
	return &ProviderSettingsRepository{pool: pool}
}

func (r *ProviderSettingsRepository) Get(ctx context.Context, provider string) (ProviderSettingsRecord, error) {
	var item ProviderSettingsRecord
	var updatedBy *string
	err := r.pool.QueryRow(ctx, `
		SELECT provider, enabled, config_json, secret_ciphertext, secret_nonce,
		       secret_key_version, updated_by_user_id::text, created_at, updated_at
		FROM delivery_provider_settings
		WHERE provider=$1
	`, normalizeProviderName(provider)).Scan(
		&item.Provider,
		&item.Enabled,
		&item.ConfigJSON,
		&item.SecretCiphertext,
		&item.SecretNonce,
		&item.SecretKeyVersion,
		&updatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if updatedBy != nil {
		item.UpdatedByUserID = *updatedBy
	}
	return item, err
}

func (r *ProviderSettingsRepository) Upsert(ctx context.Context, item ProviderSettingsRecord) (ProviderSettingsRecord, error) {
	if len(item.ConfigJSON) == 0 {
		item.ConfigJSON = []byte(`{}`)
	}
	var updatedBy any
	if strings.TrimSpace(item.UpdatedByUserID) != "" {
		updatedBy = item.UpdatedByUserID
	}
	var result ProviderSettingsRecord
	var resultUpdatedBy *string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO delivery_provider_settings (
			provider, enabled, config_json, secret_ciphertext, secret_nonce,
			secret_key_version, updated_by_user_id
		) VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7::uuid)
		ON CONFLICT (provider) DO UPDATE SET
			enabled=EXCLUDED.enabled,
			config_json=EXCLUDED.config_json,
			secret_ciphertext=EXCLUDED.secret_ciphertext,
			secret_nonce=EXCLUDED.secret_nonce,
			secret_key_version=EXCLUDED.secret_key_version,
			updated_by_user_id=EXCLUDED.updated_by_user_id,
			updated_at=NOW()
		RETURNING provider, enabled, config_json, secret_ciphertext, secret_nonce,
		          secret_key_version, updated_by_user_id::text, created_at, updated_at
	`, normalizeProviderName(item.Provider), item.Enabled, item.ConfigJSON, nullableBytes(item.SecretCiphertext), nullableBytes(item.SecretNonce), item.SecretKeyVersion, updatedBy).Scan(
		&result.Provider,
		&result.Enabled,
		&result.ConfigJSON,
		&result.SecretCiphertext,
		&result.SecretNonce,
		&result.SecretKeyVersion,
		&resultUpdatedBy,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if resultUpdatedBy != nil {
		result.UpdatedByUserID = *resultUpdatedBy
	}
	return result, err
}

func (r *ProviderSettingsRepository) Delete(ctx context.Context, provider string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM delivery_provider_settings WHERE provider=$1`, normalizeProviderName(provider))
	return err
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

type smtpManagedConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username,omitempty"`
	FromAddress string `json:"fromAddress"`
	FromName    string `json:"fromName"`
	TLSMode     string `json:"tlsMode"`
}

type telegramManagedConfig struct{}

type providerSecretEnvelope struct {
	SMTPPassword     string `json:"smtpPassword,omitempty"`
	TelegramBotToken string `json:"telegramBotToken,omitempty"`
}

type ProviderSettingsRequest struct {
	Enabled *bool           `json:"enabled,omitempty"`
	Config  json.RawMessage `json:"config"`
	Secret  *string         `json:"secret,omitempty"`
}

type ProviderSettingsView struct {
	Provider           string         `json:"provider"`
	Channel            string         `json:"channel"`
	Source             string         `json:"source"`
	Enabled            bool           `json:"enabled"`
	Configured         bool           `json:"configured"`
	Ready              bool           `json:"ready"`
	SecretConfigured   bool           `json:"secretConfigured"`
	ConfigurationError string         `json:"configurationError,omitempty"`
	Config             map[string]any `json:"config"`
}

type ProviderTestResponse struct {
	OK        bool   `json:"ok"`
	ErrorCode string `json:"errorCode,omitempty"`
}

type ProviderSettingsManager struct {
	repository *ProviderSettingsRepository
	box        *secrets.Box
	boxError   error
	legacy     config.Config
	logger     *slog.Logger
}

func NewProviderSettingsManager(pool *pgxpool.Pool, cfg config.Config, logger *slog.Logger) *ProviderSettingsManager {
	box, err := secrets.LoadBox(cfg.SecretsKeyFile)
	if err != nil && logger != nil {
		logger.Warn("managed secret store unavailable; legacy delivery environment configuration remains available", "component", "secret_store")
	}
	return &ProviderSettingsManager{
		repository: NewProviderSettingsRepository(pool),
		box:        box,
		boxError:   err,
		legacy:     cfg,
		logger:     logger,
	}
}

func (m *ProviderSettingsManager) Resolve(ctx context.Context, providerName string) (Provider, bool, error) {
	providerName = normalizeProviderName(providerName)
	if !supportedProviderName(providerName) {
		return nil, false, nil
	}
	record, err := m.repository.Get(ctx, providerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return m.legacyProvider(providerName), true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !record.Enabled {
		return newUnavailableProvider(providerName, "delivery_provider_disabled"), true, nil
	}
	provider, code := m.providerFromRecord(record)
	if code != "" {
		return newUnavailableProvider(providerName, code), true, nil
	}
	return provider, true, nil
}

func (m *ProviderSettingsManager) List(ctx context.Context) ([]ProviderInfo, error) {
	items := make([]ProviderInfo, 0, len(canonicalProviderNames))
	for _, providerName := range canonicalProviderNames {
		view, err := m.View(ctx, providerName)
		if err != nil {
			return nil, err
		}
		provider, _, err := m.Resolve(ctx, providerName)
		if err != nil {
			return nil, err
		}
		info := ProviderInfo{
			Name:               providerName,
			Channel:            channelForProvider(providerName),
			Configured:         view.Configured,
			ConfigurationError: view.ConfigurationError,
			Source:             view.Source,
			SecretConfigured:   view.SecretConfigured,
		}
		if capable, ok := provider.(capableProvider); ok {
			info.Capabilities = capable.Capabilities()
		}
		items = append(items, info)
	}
	return items, nil
}

func (m *ProviderSettingsManager) View(ctx context.Context, providerName string) (ProviderSettingsView, error) {
	providerName = normalizeProviderName(providerName)
	if !supportedProviderName(providerName) {
		return ProviderSettingsView{}, fmt.Errorf("unsupported delivery provider")
	}
	record, err := m.repository.Get(ctx, providerName)
	if errors.Is(err, pgx.ErrNoRows) {
		provider := m.legacyProvider(providerName)
		configured, configurationError := providerConfiguration(provider)
		return ProviderSettingsView{
			Provider:           providerName,
			Channel:            channelForProvider(providerName),
			Source:             legacySource(providerName, m.legacy),
			Enabled:            true,
			Configured:         configured,
			Ready:              configured,
			SecretConfigured:   legacySecretConfigured(providerName, m.legacy),
			ConfigurationError: configurationError,
			Config:             safeLegacyConfig(providerName, m.legacy),
		}, nil
	}
	if err != nil {
		return ProviderSettingsView{}, err
	}
	provider, code := m.providerFromRecord(record)
	configured := false
	configurationError := code
	if code == "" && record.Enabled {
		configured, configurationError = providerConfiguration(provider)
	}
	if !record.Enabled {
		configurationError = "delivery_provider_disabled"
	}
	return ProviderSettingsView{
		Provider:           providerName,
		Channel:            channelForProvider(providerName),
		Source:             providerSourceManaged,
		Enabled:            record.Enabled,
		Configured:         configured,
		Ready:              configured && record.Enabled,
		SecretConfigured:   len(record.SecretCiphertext) > 0,
		ConfigurationError: configurationError,
		Config:             safeConfigFromJSON(providerName, record.ConfigJSON),
	}, nil
}

func (m *ProviderSettingsManager) Save(ctx context.Context, providerName string, request ProviderSettingsRequest, updatedBy string) (ProviderSettingsView, error) {
	providerName = normalizeProviderName(providerName)
	if !supportedProviderName(providerName) {
		return ProviderSettingsView{}, Failure{Class: ErrorClassPermanent, Code: "delivery_provider_unsupported"}
	}
	if m.box == nil {
		return ProviderSettingsView{}, Failure{Class: ErrorClassPermanent, Code: "secret_store_unavailable"}
	}

	existing, existingErr := m.repository.Get(ctx, providerName)
	if existingErr != nil && !errors.Is(existingErr, pgx.ErrNoRows) {
		return ProviderSettingsView{}, existingErr
	}

	enabled := true
	if existingErr == nil {
		enabled = existing.Enabled
	}
	if request.Enabled != nil {
		enabled = *request.Enabled
	}

	configJSON, err := normalizeManagedConfigJSON(providerName, request.Config)
	if err != nil {
		return ProviderSettingsView{}, Failure{Class: ErrorClassPermanent, Code: providerConfigInvalidCode(providerName)}
	}

	secretEnvelope, err := m.baseSecretEnvelope(providerName, existing, existingErr)
	if err != nil {
		return ProviderSettingsView{}, err
	}
	if request.Secret != nil {
		setProviderSecret(providerName, &secretEnvelope, *request.Secret)
	}

	provider, code := providerFromManagedParts(providerName, configJSON, secretEnvelope)
	if code != "" {
		return ProviderSettingsView{}, Failure{Class: ErrorClassPermanent, Code: code}
	}
	if configured, configurationError := providerConfiguration(provider); !configured {
		return ProviderSettingsView{}, Failure{Class: ErrorClassPermanent, Code: configurationError}
	}

	ciphertext, nonce, err := m.encryptEnvelope(providerName, secretEnvelope)
	if err != nil {
		return ProviderSettingsView{}, Failure{Class: ErrorClassPermanent, Code: "secret_store_unavailable"}
	}
	_, err = m.repository.Upsert(ctx, ProviderSettingsRecord{
		Provider:         providerName,
		Enabled:          enabled,
		ConfigJSON:       configJSON,
		SecretCiphertext: ciphertext,
		SecretNonce:      nonce,
		SecretKeyVersion: secrets.CurrentVersion,
		UpdatedByUserID:  strings.TrimSpace(updatedBy),
	})
	if err != nil {
		return ProviderSettingsView{}, err
	}
	return m.View(ctx, providerName)
}

func (m *ProviderSettingsManager) Delete(ctx context.Context, providerName string) error {
	providerName = normalizeProviderName(providerName)
	if !supportedProviderName(providerName) {
		return Failure{Class: ErrorClassPermanent, Code: "delivery_provider_unsupported"}
	}
	return m.repository.Delete(ctx, providerName)
}

func (m *ProviderSettingsManager) Test(ctx context.Context, providerName string, request ProviderSettingsRequest) ProviderTestResponse {
	providerName = normalizeProviderName(providerName)
	if !supportedProviderName(providerName) {
		return ProviderTestResponse{OK: false, ErrorCode: "delivery_provider_unsupported"}
	}

	existing, existingErr := m.repository.Get(ctx, providerName)
	if existingErr != nil && !errors.Is(existingErr, pgx.ErrNoRows) {
		return ProviderTestResponse{OK: false, ErrorCode: "delivery_storage_error"}
	}
	configJSON, err := normalizeManagedConfigJSON(providerName, request.Config)
	if err != nil {
		return ProviderTestResponse{OK: false, ErrorCode: providerConfigInvalidCode(providerName)}
	}
	secretEnvelope, err := m.baseSecretEnvelope(providerName, existing, existingErr)
	if err != nil {
		return ProviderTestResponse{OK: false, ErrorCode: failureFromError(err, ErrorClassPermanent, "secret_store_unavailable").Code}
	}
	if request.Secret != nil {
		setProviderSecret(providerName, &secretEnvelope, *request.Secret)
	}
	provider, code := providerFromManagedParts(providerName, configJSON, secretEnvelope)
	if code != "" {
		return ProviderTestResponse{OK: false, ErrorCode: code}
	}
	configured, configurationError := providerConfiguration(provider)
	if !configured {
		return ProviderTestResponse{OK: false, ErrorCode: configurationError}
	}
	testable, ok := provider.(testableProvider)
	if !ok {
		return ProviderTestResponse{OK: true}
	}
	result := testable.Test(ctx)
	if result.Outcome == OutcomeAccepted || result.Outcome == OutcomeDelivered {
		return ProviderTestResponse{OK: true}
	}
	return ProviderTestResponse{OK: false, ErrorCode: normalizeSafeCode(result.ErrorCode)}
}

func (m *ProviderSettingsManager) providerFromRecord(record ProviderSettingsRecord) (Provider, string) {
	if record.SecretKeyVersion != secrets.CurrentVersion {
		return nil, "secret_key_version_unsupported"
	}
	envelope := providerSecretEnvelope{}
	if len(record.SecretCiphertext) > 0 {
		if m.box == nil {
			return nil, "secret_store_unavailable"
		}
		plaintext, err := m.box.Open(record.SecretCiphertext, record.SecretNonce, providerSecretAAD(record.Provider, record.SecretKeyVersion))
		if err != nil || json.Unmarshal(plaintext, &envelope) != nil {
			return nil, "provider_secret_decryption_failed"
		}
	}
	return providerFromManagedParts(record.Provider, record.ConfigJSON, envelope)
}

func (m *ProviderSettingsManager) baseSecretEnvelope(providerName string, existing ProviderSettingsRecord, existingErr error) (providerSecretEnvelope, error) {
	if existingErr == nil && len(existing.SecretCiphertext) > 0 {
		if m.box == nil {
			return providerSecretEnvelope{}, Failure{Class: ErrorClassPermanent, Code: "secret_store_unavailable"}
		}
		plaintext, err := m.box.Open(existing.SecretCiphertext, existing.SecretNonce, providerSecretAAD(providerName, existing.SecretKeyVersion))
		if err != nil {
			return providerSecretEnvelope{}, Failure{Class: ErrorClassPermanent, Code: "provider_secret_decryption_failed"}
		}
		var envelope providerSecretEnvelope
		if err := json.Unmarshal(plaintext, &envelope); err != nil {
			return providerSecretEnvelope{}, Failure{Class: ErrorClassPermanent, Code: "provider_secret_decryption_failed"}
		}
		return envelope, nil
	}
	return legacySecretEnvelope(providerName, m.legacy), nil
}

func (m *ProviderSettingsManager) encryptEnvelope(providerName string, envelope providerSecretEnvelope) ([]byte, []byte, error) {
	if providerSecretEmpty(providerName, envelope) {
		return nil, nil, nil
	}
	plaintext, err := json.Marshal(envelope)
	if err != nil {
		return nil, nil, err
	}
	return m.box.Seal(plaintext, providerSecretAAD(providerName, secrets.CurrentVersion))
}

func (m *ProviderSettingsManager) legacyProvider(providerName string) Provider {
	switch providerName {
	case "smtp":
		return NewSMTPProvider(m.legacy.SMTP)
	case "telegram":
		return NewTelegramProvider(m.legacy.Telegram)
	default:
		return nil
	}
}

func providerFromManagedParts(providerName string, configJSON []byte, envelope providerSecretEnvelope) (Provider, string) {
	switch providerName {
	case "smtp":
		var managed smtpManagedConfig
		if err := json.Unmarshal(configJSON, &managed); err != nil {
			return nil, "smtp_configuration_invalid"
		}
		return NewSMTPProvider(config.SMTPConfig{
			Host:        managed.Host,
			Port:        managed.Port,
			Username:    managed.Username,
			Password:    envelope.SMTPPassword,
			FromAddress: managed.FromAddress,
			FromName:    managed.FromName,
			TLSMode:     managed.TLSMode,
		}), ""
	case "telegram":
		var managed telegramManagedConfig
		if err := json.Unmarshal(configJSON, &managed); err != nil {
			return nil, "telegram_configuration_invalid"
		}
		return NewTelegramProvider(config.TelegramConfig{BotToken: envelope.TelegramBotToken}), ""
	default:
		return nil, "delivery_provider_unsupported"
	}
}

func normalizeManagedConfigJSON(providerName string, raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	switch providerName {
	case "smtp":
		var value smtpManagedConfig
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		value.Host = strings.TrimSpace(value.Host)
		value.Username = strings.TrimSpace(value.Username)
		value.FromAddress = strings.TrimSpace(value.FromAddress)
		value.FromName = strings.TrimSpace(value.FromName)
		value.TLSMode = strings.ToLower(strings.TrimSpace(value.TLSMode))
		return json.Marshal(value)
	case "telegram":
		return json.Marshal(telegramManagedConfig{})
	default:
		return nil, fmt.Errorf("unsupported provider")
	}
}

func safeConfigFromJSON(providerName string, raw []byte) map[string]any {
	var value map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == nil {
		return map[string]any{}
	}
	return value
}

func safeLegacyConfig(providerName string, cfg config.Config) map[string]any {
	switch providerName {
	case "smtp":
		return map[string]any{
			"host": cfg.SMTP.Host,
			"port": cfg.SMTP.Port,
			"username": cfg.SMTP.Username,
			"fromAddress": cfg.SMTP.FromAddress,
			"fromName": cfg.SMTP.FromName,
			"tlsMode": cfg.SMTP.TLSMode,
		}
	default:
		return map[string]any{}
	}
}

func legacySource(providerName string, cfg config.Config) string {
	if legacyHasAnyConfig(providerName, cfg) {
		return providerSourceEnvironment
	}
	return providerSourceNone
}

func legacyHasAnyConfig(providerName string, cfg config.Config) bool {
	switch providerName {
	case "smtp":
		return strings.TrimSpace(cfg.SMTP.Host) != "" || strings.TrimSpace(cfg.SMTP.Username) != "" || strings.TrimSpace(cfg.SMTP.Password) != "" || strings.TrimSpace(cfg.SMTP.FromAddress) != ""
	case "telegram":
		return strings.TrimSpace(cfg.Telegram.BotToken) != ""
	default:
		return false
	}
}

func legacySecretConfigured(providerName string, cfg config.Config) bool {
	switch providerName {
	case "smtp":
		return strings.TrimSpace(cfg.SMTP.Password) != ""
	case "telegram":
		return strings.TrimSpace(cfg.Telegram.BotToken) != ""
	default:
		return false
	}
}

func legacySecretEnvelope(providerName string, cfg config.Config) providerSecretEnvelope {
	envelope := providerSecretEnvelope{}
	switch providerName {
	case "smtp":
		envelope.SMTPPassword = cfg.SMTP.Password
	case "telegram":
		envelope.TelegramBotToken = cfg.Telegram.BotToken
	}
	return envelope
}

func setProviderSecret(providerName string, envelope *providerSecretEnvelope, value string) {
	if envelope == nil {
		return
	}
	switch providerName {
	case "smtp":
		envelope.SMTPPassword = value
	case "telegram":
		envelope.TelegramBotToken = strings.TrimSpace(value)
	}
}

func providerSecretEmpty(providerName string, envelope providerSecretEnvelope) bool {
	switch providerName {
	case "smtp":
		return envelope.SMTPPassword == ""
	case "telegram":
		return strings.TrimSpace(envelope.TelegramBotToken) == ""
	default:
		return true
	}
}

func providerSecretAAD(providerName string, version int) []byte {
	return []byte(fmt.Sprintf("routegate:delivery-provider-settings:%s:v%d", normalizeProviderName(providerName), version))
}

func providerConfiguration(provider Provider) (bool, string) {
	configured, ok := provider.(configurableProvider)
	if !ok {
		return provider != nil, ""
	}
	if configured.Configured() {
		return true, ""
	}
	return false, normalizeSafeCode(configured.ConfigurationErrorCode())
}

func supportedProviderName(value string) bool {
	switch normalizeProviderName(value) {
	case "smtp", "telegram":
		return true
	default:
		return false
	}
}

func normalizeProviderName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func channelForProvider(providerName string) string {
	switch normalizeProviderName(providerName) {
	case "smtp":
		return "email"
	case "telegram":
		return "telegram"
	default:
		return ""
	}
}

func providerConfigInvalidCode(providerName string) string {
	switch normalizeProviderName(providerName) {
	case "smtp":
		return "smtp_configuration_invalid"
	case "telegram":
		return "telegram_configuration_invalid"
	default:
		return "delivery_provider_unsupported"
	}
}

type unavailableProvider struct {
	name         string
	channel      string
	errorCode    string
	capabilities ProviderCapabilities
}

func newUnavailableProvider(providerName, code string) *unavailableProvider {
	providerName = normalizeProviderName(providerName)
	capabilities := ProviderCapabilities{}
	switch providerName {
	case "smtp":
		capabilities = ProviderCapabilities{HTML: true, Attachments: true, DeliveryReceipts: false}
	}
	return &unavailableProvider{
		name:         providerName,
		channel:      channelForProvider(providerName),
		errorCode:    normalizeSafeCode(code),
		capabilities: capabilities,
	}
}

func (p *unavailableProvider) Name() string    { return p.name }
func (p *unavailableProvider) Channel() string { return p.channel }
func (p *unavailableProvider) Capabilities() ProviderCapabilities { return p.capabilities }
func (p *unavailableProvider) Configured() bool { return false }
func (p *unavailableProvider) ConfigurationErrorCode() string { return p.errorCode }
func (p *unavailableProvider) Send(context.Context, Message) ProviderResult {
	return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: p.errorCode}
}
