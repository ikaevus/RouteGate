package delivery

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

func (h *Handler) GetProviderSettings(w http.ResponseWriter, r *http.Request) {
	providerName := normalizeProviderName(r.PathValue("provider"))
	if !supportedProviderName(providerName) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("delivery_provider_unsupported", "This delivery provider is not supported."))
		return
	}
	view, err := h.settings.View(r.Context(), providerName)
	if err != nil {
		h.databaseError(w, "get_delivery_provider_settings")
		return
	}
	if view.Ready {
		if _, err := NormalizePublicURL(h.publicURL); err != nil {
			view.Ready = false
			view.ConfigurationError = failureFromError(err, ErrorClassPermanent, "public_url_invalid").Code
		}
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

func (h *Handler) PutProviderSettings(w http.ResponseWriter, r *http.Request) {
	providerName := normalizeProviderName(r.PathValue("provider"))
	if !supportedProviderName(providerName) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("delivery_provider_unsupported", "This delivery provider is not supported."))
		return
	}
	request, ok := decodeProviderSettingsRequest(w, r)
	if !ok {
		return
	}
	updatedBy := ""
	if user, exists := auth.UserFromContext(r.Context()); exists {
		updatedBy = user.ID
	}
	view, err := h.settings.Save(r.Context(), providerName, request, updatedBy)
	if err != nil {
		if h.writeProviderSettingsFailure(w, err) {
			return
		}
		h.databaseError(w, "save_delivery_provider_settings")
		return
	}
	h.recordAudit(r, audit.EventInput{
		Action:       "delivery.provider_settings.updated",
		ResourceType: "delivery_provider",
		ResourceID:   providerName,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"provider": providerName,
			"enabled": view.Enabled,
			"source": providerSourceManaged,
			"secret_configured": view.SecretConfigured,
		},
	})
	if view.Ready {
		if _, err := NormalizePublicURL(h.publicURL); err != nil {
			view.Ready = false
			view.ConfigurationError = failureFromError(err, ErrorClassPermanent, "public_url_invalid").Code
		}
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

func (h *Handler) TestProviderSettings(w http.ResponseWriter, r *http.Request) {
	providerName := normalizeProviderName(r.PathValue("provider"))
	if !supportedProviderName(providerName) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("delivery_provider_unsupported", "This delivery provider is not supported."))
		return
	}
	request, ok := decodeProviderSettingsRequest(w, r)
	if !ok {
		return
	}
	result := h.settings.Test(r.Context(), providerName, request)
	h.recordAudit(r, audit.EventInput{
		Action:       "delivery.provider_settings.tested",
		ResourceType: "delivery_provider",
		ResourceID:   providerName,
		Result:       map[bool]string{true: audit.ResultSuccess, false: audit.ResultFailure}[result.OK],
		Metadata: map[string]any{
			"provider": providerName,
			"ok": result.OK,
			"error_code": normalizeSafeCode(result.ErrorCode),
		},
	})
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) DeleteProviderSettings(w http.ResponseWriter, r *http.Request) {
	providerName := normalizeProviderName(r.PathValue("provider"))
	if !supportedProviderName(providerName) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("delivery_provider_unsupported", "This delivery provider is not supported."))
		return
	}
	if err := h.settings.Delete(r.Context(), providerName); err != nil {
		if h.writeProviderSettingsFailure(w, err) {
			return
		}
		h.databaseError(w, "delete_delivery_provider_settings")
		return
	}
	h.recordAudit(r, audit.EventInput{
		Action:       "delivery.provider_settings.removed",
		ResourceType: "delivery_provider",
		ResourceID:   providerName,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"provider": providerName,
			"fallback": providerSourceEnvironment,
		},
	})
	w.WriteHeader(http.StatusNoContent)
}

func decodeProviderSettingsRequest(w http.ResponseWriter, r *http.Request) (ProviderSettingsRequest, bool) {
	var request ProviderSettingsRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must be valid JSON."))
		return ProviderSettingsRequest{}, false
	}
	if len(request.Config) == 0 {
		request.Config = json.RawMessage(`{}`)
	}
	return request, true
}

func (h *Handler) writeProviderSettingsFailure(w http.ResponseWriter, err error) bool {
	var failure Failure
	if !errors.As(err, &failure) {
		return false
	}
	code := normalizeSafeCode(failure.Code)
	status := http.StatusBadRequest
	message := "Delivery provider configuration is invalid."
	switch code {
	case "secret_store_unavailable":
		status = http.StatusServiceUnavailable
		message = "RouteGate secure settings storage is unavailable."
	case "provider_secret_decryption_failed", "secret_key_version_unsupported":
		status = http.StatusInternalServerError
		message = "RouteGate could not read the stored provider credential."
	case "delivery_provider_unsupported":
		status = http.StatusNotFound
		message = "This delivery provider is not supported."
	case "smtp_not_configured", "smtp_configuration_invalid",
		"telegram_not_configured", "telegram_configuration_invalid",
		"whatsapp_not_configured", "whatsapp_configuration_invalid":
		status = http.StatusBadRequest
	default:
		if strings.TrimSpace(code) == "" {
			code = "delivery_provider_configuration_invalid"
		}
	}
	httpx.WriteJSON(w, status, httpx.Error(code, message))
	return true
}
