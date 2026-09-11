package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/config"
	"github.com/ikaevus/routegate/backend/internal/httpx"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

type CreateDeliveryRequest struct {
	Channel   string `json:"channel"`
	Recipient string `json:"recipient"`
	Locale    string `json:"locale"`
	Template  string `json:"template"`
	AttachQR  bool   `json:"attachQr"`
	// DeviceID and AccessURL scope this Send to one Access & Devices device.
	// AccessURL must be the exact plaintext link the caller currently holds
	// for that device (RG-115 tokens are hash-only server-side, so RouteGate
	// cannot look it up); it is validated against the device's own active
	// token hash before anything is sent. Omit both for account-level Send.
	DeviceID  string `json:"deviceId,omitempty"`
	AccessURL string `json:"accessUrl,omitempty"`
}

type ProviderResponse struct {
	Name               string               `json:"name"`
	Channel            string               `json:"channel"`
	Configured         bool                 `json:"configured"`
	Ready              bool                 `json:"ready"`
	ConfigurationError string               `json:"configurationError,omitempty"`
	Capabilities       ProviderCapabilities `json:"capabilities"`
	Source             string               `json:"source,omitempty"`
	SecretConfigured   bool                 `json:"secretConfigured"`
}

type ProviderListResponse struct {
	Items []ProviderResponse `json:"items"`
}

type DeliveryResponse struct {
	ID               string     `json:"id"`
	VPNAccountID     string     `json:"vpnAccountId,omitempty"`
	DeviceID         string     `json:"deviceId,omitempty"`
	Channel          string     `json:"channel"`
	Provider         string     `json:"provider"`
	RecipientDisplay string     `json:"recipientDisplay"`
	Template         string     `json:"template"`
	Locale           string     `json:"locale"`
	AttachQR         bool       `json:"attachQr"`
	Status           string     `json:"status"`
	AttemptCount     int        `json:"attemptCount"`
	MaxAttempts      int        `json:"maxAttempts"`
	NextAttemptAt    *time.Time `json:"nextAttemptAt,omitempty"`
	LastErrorClass   string     `json:"lastErrorClass,omitempty"`
	LastErrorCode    string     `json:"lastErrorCode,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	SentAt           *time.Time `json:"sentAt,omitempty"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
}

type DeliveryListResponse struct {
	Items []DeliveryResponse `json:"items"`
}

type Handler struct {
	logger     *slog.Logger
	repository *Repository
	service    *Service
	providers  providerResolver
	settings   *ProviderSettingsManager
	resolver   *VPNAccessResolver
	audit      *audit.Recorder
	publicURL  string
	accounts   *vpnaccounts.Repository
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool, cfg config.Config) *Handler {
	repository := NewRepository(pool)
	recorder := audit.NewRecorder(logger, pool)
	providers := NewProviderSettingsManager(pool, cfg, logger)
	accounts := vpnaccounts.NewRepository(pool)
	return &Handler{
		logger:     logger,
		repository: repository,
		service:    NewService(repository, recorder),
		providers:  providers,
		settings:   providers,
		resolver:   NewVPNAccessResolver(accounts, cfg.PublicURL),
		audit:      recorder,
		publicURL:  cfg.PublicURL,
		accounts:   accounts,
	}
}

func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	items, err := h.providers.List(r.Context())
	if err != nil {
		h.databaseError(w, "list_delivery_providers", err)
		return
	}
	response := ProviderListResponse{Items: make([]ProviderResponse, 0, len(items))}
	for _, item := range items {
		provider := ProviderResponse{
			Name:               item.Name,
			Channel:            item.Channel,
			Configured:         item.Configured,
			Ready:              item.Configured,
			ConfigurationError: item.ConfigurationError,
			Capabilities:       item.Capabilities,
			Source:             item.Source,
			SecretConfigured:   item.SecretConfigured,
		}
		if provider.Ready {
			if _, err := NormalizePublicURL(h.publicURL); err != nil {
				provider.Ready = false
				provider.ConfigurationError = failureFromError(err, ErrorClassPermanent, "public_url_invalid").Code
			}
		}
		response.Items = append(response.Items, provider)
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) CreateForVPNAccount(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.PathValue("id"))
	if _, err := h.accounts.GetAccountByID(r.Context(), accountID); errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("vpn_account_not_found", "VPN account not found."))
		return
	} else if err != nil {
		h.databaseError(w, "read_vpn_account", err)
		return
	}

	var request CreateDeliveryRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must be valid JSON."))
		return
	}
	request.Channel = strings.ToLower(strings.TrimSpace(request.Channel))
	request.Locale = strings.ToLower(strings.TrimSpace(request.Locale))
	request.Template = strings.ToLower(strings.TrimSpace(request.Template))
	request.Recipient = strings.TrimSpace(request.Recipient)

	if request.Template != TemplateVPNAccess && request.Template != TemplateVPNAccessReissued {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("delivery_template_unsupported", "This VPN access template is not supported."))
		return
	}
	if !validLocale(request.Locale) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("delivery_locale_unsupported", "Delivery locale must be en or ru."))
		return
	}

	providerName, recipient, recipientErr := normalizeChannelRecipient(request.Channel, request.Recipient)
	if recipientErr != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error(recipientErr.Code, recipientErr.Message))
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(idempotencyKey) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("idempotency_key_required", "A valid Idempotency-Key header is required."))
		return
	}

	provider, ok, err := h.providers.Resolve(r.Context(), providerName)
	if err != nil {
		h.databaseError(w, "resolve_delivery_provider", err)
		return
	}
	if !ok || provider == nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("delivery_provider_unavailable", "This delivery provider is not available."))
		return
	}
	if configured, ok := provider.(configurableProvider); !ok || !configured.Configured() {
		code := "delivery_provider_not_configured"
		if ok {
			code = normalizeSafeCode(configured.ConfigurationErrorCode())
		}
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error(code, "This delivery provider is not ready."))
		return
	}
	if request.AttachQR {
		capable, ok := provider.(capableProvider)
		if !ok || !capable.Capabilities().Attachments {
			httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("delivery_attachment_unsupported", "This delivery channel does not support QR attachments."))
			return
		}
	}

	deviceID := strings.TrimSpace(request.DeviceID)
	var deviceProfileName, deviceAccessURL string
	if deviceID != "" {
		// Access & Devices: the device card owns Send. The caller must pass
		// the exact plaintext access URL it currently holds in memory (RG-115
		// tokens are hash-only; RouteGate cannot reconstruct it), and that URL
		// must hash to the device's own current active token so an admin
		// session cannot use this endpoint to relay an arbitrary URL through
		// RouteGate's mail/Telegram sender.
		profileName, resolvedAccessURL, deviceErr := h.validateDeviceAccessRequest(r.Context(), accountID, deviceID, request.AccessURL)
		if deviceErr != nil {
			httpx.WriteJSON(w, deviceErr.status, httpx.Error(deviceErr.code, deviceErr.message))
			return
		}
		deviceProfileName, deviceAccessURL = profileName, resolvedAccessURL
	} else {
		if _, err := NormalizePublicURL(h.publicURL); err != nil {
			failure := failureFromError(err, ErrorClassPermanent, "public_url_invalid")
			httpx.WriteJSON(w, http.StatusConflict, httpx.Error(failure.Code, "RouteGate public URL is not ready for access delivery."))
			return
		}
		preflight := Delivery{VPNAccountID: accountID, TemplateKey: request.Template, AttachQR: false}
		if _, err := h.resolver.Resolve(r.Context(), preflight); err != nil {
			failure := failureFromError(err, ErrorClassPermanent, "vpn_access_unavailable")
			httpx.WriteJSON(w, http.StatusConflict, httpx.Error(failure.Code, "VPN access is not ready to send yet."))
			return
		}
	}

	createdBy := ""
	if user, ok := auth.UserFromContext(r.Context()); ok {
		createdBy = user.ID
	}
	delivery, _, createErr := h.service.Create(r.Context(), CreateInput{
		VPNAccountID:    accountID,
		DeviceID:        deviceID,
		Channel:         request.Channel,
		Provider:        providerName,
		Recipient:       recipient,
		TemplateKey:     request.Template,
		Locale:          request.Locale,
		AttachQR:        request.AttachQR,
		IdempotencyKey:  idempotencyKey,
		CreatedByUserID: createdBy,
	})
	if errors.Is(createErr, ErrIdempotencyConflict) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("idempotency_conflict", "Idempotency-Key was already used for a different delivery request."))
		return
	}
	if createErr != nil {
		h.databaseError(w, "create_delivery", createErr)
		return
	}
	if deviceID != "" {
		// Stash after the row exists (its ID is the store key) and only once
		// we know the request will actually be enqueued; nothing here is
		// persisted to the database.
		h.resolver.StashDeviceAccess(delivery.ID, deviceAccessURL, deviceProfileName)
	}
	httpx.WriteJSON(w, http.StatusAccepted, toDeliveryResponse(delivery))
}

// deviceAccessRequestError is a small HTTP-shaped error for device-scoped
// Send validation, distinct from channelRecipientError so callers can tell
// device validation failures apart from channel/recipient ones.
type deviceAccessRequestError struct {
	status  int
	code    string
	message string
}

// validateDeviceAccessRequest confirms the caller-supplied access URL is
// genuinely the device's own, currently active RG-115 link before letting it
// anywhere near the mail/Telegram pipeline: the device must belong to this
// account and be active, the URL must parse as a /sub/<token> link, and that
// token must hash to the device's current active token. This never reads or
// stores the plaintext token; it only re-hashes the caller's own value to
// compare against the existing hash-only row.
func (h *Handler) validateDeviceAccessRequest(ctx context.Context, accountID, deviceID, accessURL string) (string, string, *deviceAccessRequestError) {
	accessURL = strings.TrimSpace(accessURL)
	if accessURL == "" {
		return "", "", &deviceAccessRequestError{http.StatusBadRequest, "device_access_url_required", "accessUrl is required when sending for a specific device."}
	}

	device, err := h.accounts.GetDevice(ctx, accountID, deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", &deviceAccessRequestError{http.StatusNotFound, "device_not_found", "Device not found."}
	}
	if err != nil {
		return "", "", &deviceAccessRequestError{http.StatusInternalServerError, "database_error", "Database operation failed."}
	}
	if device.Status != vpnaccounts.DeviceStatusActive {
		return "", "", &deviceAccessRequestError{http.StatusConflict, "device_revoked", "This device has been revoked."}
	}

	token, err := h.accounts.GetActiveDeviceSubscriptionToken(ctx, deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", &deviceAccessRequestError{http.StatusConflict, "device_access_url_stale", "This device has no active access link. Rotate to create one, then send again."}
	}
	if err != nil {
		return "", "", &deviceAccessRequestError{http.StatusInternalServerError, "database_error", "Database operation failed."}
	}

	rawToken, parseErr := extractSubscriptionToken(accessURL)
	if parseErr != nil || vpnaccounts.HashSubscriptionToken(rawToken) != token.TokenHash {
		return "", "", &deviceAccessRequestError{http.StatusConflict, "device_access_url_stale", "This access link is no longer the device's current one. Rotate to create a new link, then send again."}
	}

	return device.Name, accessURL, nil
}

// extractSubscriptionToken pulls the opaque bearer token out of an RG-115
// `.../sub/<token>` URL without assuming a specific host, so this validates
// against whatever RouteGate public URL issued the link.
func extractSubscriptionToken(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	const marker = "/sub/"
	index := strings.LastIndex(parsed.Path, marker)
	if index < 0 {
		return "", errors.New("not a subscription URL")
	}
	token := strings.TrimSpace(parsed.Path[index+len(marker):])
	if token == "" {
		return "", errors.New("empty subscription token")
	}
	return token, nil
}

type channelRecipientError struct {
	Code    string
	Message string
}

func (e *channelRecipientError) Error() string { return e.Code }

func normalizeChannelRecipient(channel, recipient string) (string, string, *channelRecipientError) {
	switch channel {
	case "email":
		normalized, err := normalizeEmailAddress(recipient)
		if err != nil {
			return "", "", &channelRecipientError{Code: "invalid_recipient", Message: "Recipient email address is invalid."}
		}
		return "smtp", normalized, nil
	case "telegram":
		normalized, err := normalizeTelegramChatID(recipient)
		if err != nil {
			return "", "", &channelRecipientError{Code: "telegram_invalid_chat_id", Message: "Telegram recipient must be a valid numeric chat ID."}
		}
		return "telegram", normalized, nil
	default:
		return "", "", &channelRecipientError{Code: "delivery_channel_unsupported", Message: "This delivery channel is not supported yet."}
	}
}

func (h *Handler) ListForVPNAccount(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.PathValue("id"))
	if _, err := h.accounts.GetAccountByID(r.Context(), accountID); errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("vpn_account_not_found", "VPN account not found."))
		return
	} else if err != nil {
		h.databaseError(w, "read_vpn_account", err)
		return
	}
	items, err := h.repository.ListForVPNAccount(r.Context(), accountID, 50)
	if err != nil {
		h.databaseError(w, "list_deliveries", err)
		return
	}
	response := DeliveryListResponse{Items: make([]DeliveryResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, toDeliveryResponse(item))
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	item, err := h.repository.Get(r.Context(), strings.TrimSpace(r.PathValue("delivery_id")))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("delivery_not_found", "Delivery not found."))
		return
	}
	if err != nil {
		h.databaseError(w, "get_delivery", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDeliveryResponse(item))
}

func (h *Handler) Retry(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("delivery_id"))
	existing, err := h.repository.Get(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("delivery_not_found", "Delivery not found."))
		return
	}
	if err != nil {
		h.databaseError(w, "get_delivery", err)
		return
	}
	if existing.Status != StatusFailed && existing.Status != StatusUncertain {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("delivery_retry_not_allowed", "Only failed or uncertain deliveries can be retried manually."))
		return
	}
	updated, err := h.repository.Requeue(r.Context(), id)
	if err != nil {
		h.databaseError(w, "retry_delivery", err)
		return
	}
	h.recordAudit(r, audit.EventInput{
		Action:       "delivery.retry_requested",
		ResourceType: "delivery",
		ResourceID:   updated.ID,
		Result:       audit.ResultSuccess,
		Metadata:     safeAuditMetadata(updated, "", ""),
	})
	httpx.WriteJSON(w, http.StatusAccepted, toDeliveryResponse(updated))
}

func (h *Handler) recordAudit(r *http.Request, input audit.EventInput) {
	if user, ok := auth.UserFromContext(r.Context()); ok {
		input.ActorUserID = user.ID
		input.ActorType = audit.ActorTypeUser
	} else if input.ActorType == "" {
		input.ActorType = audit.ActorTypeSystem
	}
	h.audit.RecordSafe(r.Context(), input)
}

func (h *Handler) databaseError(w http.ResponseWriter, operation string, err error) {
	if h.logger != nil {
		h.logger.Error("delivery storage operation failed", "operation", operation, "error", err)
	}
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("delivery_storage_error", "Delivery storage operation failed."))
}

func toDeliveryResponse(item Delivery) DeliveryResponse {
	return DeliveryResponse{
		ID:               item.ID,
		VPNAccountID:     item.VPNAccountID,
		DeviceID:         item.DeviceID,
		Channel:          item.Channel,
		Provider:         item.Provider,
		RecipientDisplay: MaskRecipient(item.Recipient),
		Template:         item.TemplateKey,
		Locale:           item.Locale,
		AttachQR:         item.AttachQR,
		Status:           string(item.Status),
		AttemptCount:     item.AttemptCount,
		MaxAttempts:      item.MaxAttempts,
		NextAttemptAt:    item.NextAttemptAt,
		LastErrorClass:   string(item.LastErrorClass),
		LastErrorCode:    item.LastErrorCode,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
		SentAt:           item.SentAt,
		CompletedAt:      item.CompletedAt,
	}
}

func validIdempotencyKey(value string) bool {
	if len(value) < 8 || len(value) > 200 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' || char == ':' {
			continue
		}
		return false
	}
	return true
}
