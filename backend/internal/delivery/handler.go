package delivery

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
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
		h.databaseError(w, "list_delivery_providers")
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
		h.databaseError(w, "read_vpn_account")
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
		h.databaseError(w, "resolve_delivery_provider")
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

	createdBy := ""
	if user, ok := auth.UserFromContext(r.Context()); ok {
		createdBy = user.ID
	}
	delivery, _, createErr := h.service.Create(r.Context(), CreateInput{
		VPNAccountID:    accountID,
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
		h.databaseError(w, "create_delivery")
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, toDeliveryResponse(delivery))
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
	case "whatsapp":
		normalized, err := normalizeWhatsAppRecipient(recipient)
		if err != nil {
			return "", "", &channelRecipientError{Code: "whatsapp_invalid_recipient", Message: "WhatsApp recipient must be an international phone number with country code."}
		}
		return "whatsapp", normalized, nil
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
		h.databaseError(w, "read_vpn_account")
		return
	}
	items, err := h.repository.ListForVPNAccount(r.Context(), accountID, 50)
	if err != nil {
		h.databaseError(w, "list_deliveries")
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
		h.databaseError(w, "get_delivery")
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
		h.databaseError(w, "get_delivery")
		return
	}
	if existing.Status != StatusFailed && existing.Status != StatusUncertain {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("delivery_retry_not_allowed", "Only failed or uncertain deliveries can be retried manually."))
		return
	}
	updated, err := h.repository.Requeue(r.Context(), id)
	if err != nil {
		h.databaseError(w, "retry_delivery")
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

func (h *Handler) databaseError(w http.ResponseWriter, operation string) {
	if h.logger != nil {
		h.logger.Error("delivery storage operation failed", "operation", operation)
	}
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("delivery_storage_error", "Delivery storage operation failed."))
}

func toDeliveryResponse(item Delivery) DeliveryResponse {
	return DeliveryResponse{
		ID:               item.ID,
		VPNAccountID:     item.VPNAccountID,
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
