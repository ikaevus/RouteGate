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
	var deviceProfileName, deviceAccessURL, deviceTokenID string
	if deviceID != "" {
		// Access & Devices: the device card owns Send. The caller must pass
		// the exact plaintext access URL it currently holds in memory (RG-115
		// tokens are hash-only; RouteGate cannot reconstruct it), and that URL
		// must hash to the device's own current active token so an admin
		// session cannot use this endpoint to relay an arbitrary URL through
		// RouteGate's mail/Telegram sender.
		profileName, resolvedAccessURL, tokenID, deviceErr := h.validateDeviceAccessRequest(r.Context(), accountID, deviceID, request.AccessURL)
		if deviceErr != nil {
			httpx.WriteJSON(w, deviceErr.status, httpx.Error(deviceErr.code, deviceErr.message))
			return
		}
		deviceProfileName, deviceAccessURL, deviceTokenID = profileName, resolvedAccessURL, tokenID
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
	delivery, created, createErr := h.service.Create(r.Context(), CreateInput{
		VPNAccountID:        accountID,
		DeviceID:            deviceID,
		SubscriptionTokenID: deviceTokenID,
		Channel:             request.Channel,
		Provider:            providerName,
		Recipient:           recipient,
		TemplateKey:         request.Template,
		Locale:              request.Locale,
		AttachQR:            request.AttachQR,
		IdempotencyKey:      idempotencyKey,
		CreatedByUserID:     createdBy,
	})
	if errors.Is(createErr, ErrIdempotencyConflict) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("idempotency_conflict", "Idempotency-Key was already used for a different delivery request."))
		return
	}
	if createErr != nil {
		h.databaseError(w, "create_delivery", createErr)
		return
	}
	// Stash after the row exists (its ID is the store key) and only when the
	// plaintext material is actually still needed:
	//   - created: a brand-new delivery, always stash.
	//   - idempotent replay of an existing TERMINAL delivery (sent/delivered/
	//     failed/uncertain): never re-stash - the worker will never claim a
	//     terminal delivery again, so re-stashing would just leave the
	//     plaintext access URL sitting in memory for the rest of the TTL for
	//     no reason (undoing the point of the worker's terminal-release).
	//   - idempotent replay of an existing queued/sending/retrying delivery:
	//     the worker may still claim this delivery and will need the
	//     material, so re-stash - but only because sameCreateRequest (called
	//     inside h.service.Create) already confirmed this replay carries the
	//     same DeviceID and the same SubscriptionTokenID generation as the
	//     stored row (otherwise Create would have returned
	//     ErrIdempotencyConflict), and deviceAccessURL was itself re-hashed
	//     and matched against the device's current active token by
	//     validateDeviceAccessRequest above, in this same request. This is a
	//     deliberate recovery path, not a blind "if created" stash.
	if shouldStashDeviceAccess(deviceID, created, delivery.Status) {
		h.resolver.StashDeviceAccess(delivery.ID, deviceAccessURL, deviceProfileName)
	}
	httpx.WriteJSON(w, http.StatusAccepted, toDeliveryResponse(delivery))
}

// shouldStashDeviceAccess decides whether CreateForVPNAccount's call into
// h.service.Create should be followed by StashDeviceAccess for a
// device-scoped request. created is true only for a brand-new delivery row;
// status is the row's current status (freshly created, or - for an
// idempotent replay - whatever it already was). See the comment at the call
// site for why replaying a terminal delivery must never re-stash.
func shouldStashDeviceAccess(deviceID string, created bool, status Status) bool {
	return deviceID != "" && (created || !isTerminalStatus(status))
}

// deviceAccessRequestError is a small HTTP-shaped error for device-scoped
// Send validation, distinct from channelRecipientError so callers can tell
// device validation failures apart from channel/recipient ones.
type deviceAccessRequestError struct {
	status  int
	code    string
	message string
}

// deviceAccessLookup is the narrow slice of *vpnaccounts.Repository that
// device-scoped Send validation needs. Extracted as an interface (rather
// than calling h.accounts directly) purely so validateDeviceAccessRequestWith
// can be unit-tested with a fake in-memory repository instead of a real
// database - h.accounts stays a concrete *vpnaccounts.Repository everywhere
// else (it also serves as vpnaccounts.ClientConnectionSource for other
// handlers, so it is not converted to an interface package-wide here).
type deviceAccessLookup interface {
	GetDevice(ctx context.Context, vpnAccountID, deviceID string) (vpnaccounts.Device, error)
	GetActiveDeviceSubscriptionToken(ctx context.Context, deviceID string) (vpnaccounts.SubscriptionToken, error)
}

// validateDeviceAccessRequest confirms the caller-supplied access URL is
// genuinely the device's own, currently active RG-115 link before letting it
// anywhere near the mail/Telegram pipeline: the device must belong to this
// account and be active, the URL must be RouteGate's own canonical
// `/sub/<token>` link (not merely a URL embedding a token that hashes
// correctly - see extractCanonicalSubscriptionToken), and that token must
// hash to the device's current active token. This never reads or stores the
// plaintext token; it only re-hashes the caller's own value to compare
// against the existing hash-only row. The returned tokenID is the current
// active token's non-secret row ID, used only to detect rotation for
// delivery idempotency (see CreateInput.SubscriptionTokenID); it is never
// the token, its hash, or a substitute credential.
func (h *Handler) validateDeviceAccessRequest(ctx context.Context, accountID, deviceID, accessURL string) (profileName, resolvedAccessURL, tokenID string, failure *deviceAccessRequestError) {
	return validateDeviceAccessRequestWith(ctx, h.accounts, h.publicURL, accountID, deviceID, accessURL)
}

func validateDeviceAccessRequestWith(ctx context.Context, accounts deviceAccessLookup, publicURL, accountID, deviceID, accessURL string) (profileName, resolvedAccessURL, tokenID string, failure *deviceAccessRequestError) {
	accessURL = strings.TrimSpace(accessURL)
	if accessURL == "" {
		return "", "", "", &deviceAccessRequestError{http.StatusBadRequest, "device_access_url_required", "accessUrl is required when sending for a specific device."}
	}

	device, err := accounts.GetDevice(ctx, accountID, deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", &deviceAccessRequestError{http.StatusNotFound, "device_not_found", "Device not found."}
	}
	if err != nil {
		return "", "", "", &deviceAccessRequestError{http.StatusInternalServerError, "database_error", "Database operation failed."}
	}
	if device.Status != vpnaccounts.DeviceStatusActive {
		return "", "", "", &deviceAccessRequestError{http.StatusConflict, "device_revoked", "This device has been revoked."}
	}

	token, err := accounts.GetActiveDeviceSubscriptionToken(ctx, deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", &deviceAccessRequestError{http.StatusConflict, "device_access_url_stale", "This device has no active access link. Rotate to create one, then send again."}
	}
	if err != nil {
		return "", "", "", &deviceAccessRequestError{http.StatusInternalServerError, "database_error", "Database operation failed."}
	}

	rawToken, parseErr := extractCanonicalSubscriptionToken(publicURL, accessURL)
	if parseErr != nil || vpnaccounts.HashSubscriptionToken(rawToken) != token.TokenHash {
		return "", "", "", &deviceAccessRequestError{http.StatusConflict, "device_access_url_stale", "This access link is no longer the device's current one. Rotate to create a new link, then send again."}
	}

	return device.Name, accessURL, token.ID, nil
}

// extractCanonicalSubscriptionToken extracts the opaque bearer token from an
// RG-115 subscription URL, but only after confirming the URL is genuinely
// RouteGate's own canonical `/sub/<token>` link - not merely a URL that
// happens to embed a token that later hashes correctly. Without this check,
// a URL like `https://evil.example/sub/<valid-device-token>` would satisfy a
// token-hash-only comparison and let this endpoint relay mail/Telegram
// delivery through an attacker-chosen link. It reuses NormalizePublicURL,
// the same canonical-origin policy the rest of delivery already applies to
// connect.html links, rather than inventing a second URL-validation policy.
func extractCanonicalSubscriptionToken(publicURL, rawURL string) (string, error) {
	canonicalOrigin, err := NormalizePublicURL(publicURL)
	if err != nil {
		return "", err
	}
	canonical, err := url.Parse(canonicalOrigin)
	if err != nil {
		return "", err
	}

	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", errors.New("empty URL")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	// Reject anything that isn't an exact match on RouteGate's own
	// configured origin: wrong scheme, wrong host, or embedded userinfo
	// (which some URL parsers/clients treat as part of the authority but
	// browsers/proxies can disagree about) are all foreign-URL relay
	// attempts, not "the device's current link".
	if parsed.User != nil {
		return "", errors.New("URL must not contain userinfo")
	}
	if !strings.EqualFold(parsed.Scheme, canonical.Scheme) || !strings.EqualFold(parsed.Host, canonical.Host) {
		return "", errors.New("URL does not match the configured RouteGate public URL")
	}
	// The canonical Universal Access URL is exactly /sub/<token>: no extra
	// path segments, and - so a relay attempt cannot smuggle instructions
	// past this check via a client-specific delivery format - no query
	// parameters or fragment either.
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("URL must not contain query parameters or a fragment")
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) != 2 || segments[0] != "sub" {
		return "", errors.New("URL path is not a canonical subscription link")
	}
	token := strings.TrimSpace(segments[1])
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
