package delivery

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/httpx"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

type PreviewDeliveryRequest struct {
	Locale   string `json:"locale"`
	Template string `json:"template"`
}

type PreviewDeliveryResponse struct {
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

func (h *Handler) PreviewForVPNAccount(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.PathValue("id"))
	connection, err := vpnaccounts.BuildClientConnection(r.Context(), h.accounts, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("vpn_account_not_found", "VPN account not found."))
		return
	}
	if err != nil {
		failure := failureFromError(classifyAccessMaterialError(err), ErrorClassPermanent, "vpn_access_unavailable")
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error(failure.Code, "VPN access is not ready to preview yet."))
		return
	}

	var request PreviewDeliveryRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must be valid JSON."))
		return
	}
	request.Locale = strings.ToLower(strings.TrimSpace(request.Locale))
	request.Template = strings.ToLower(strings.TrimSpace(request.Template))
	if !validLocale(request.Locale) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("delivery_locale_unsupported", "Delivery locale must be en or ru."))
		return
	}
	if request.Template != TemplateVPNAccess && request.Template != TemplateVPNAccessReissued {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("delivery_template_unsupported", "This VPN access template is not supported."))
		return
	}

	message, err := NewRenderer().Render(request.Template, request.Locale, TemplateData{
		ProfileName: strings.TrimSpace(connection.Profile.Name),
		ConnectURL:  "[secure access link inserted when sent]",
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("delivery_preview_failed", "Delivery preview could not be rendered."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, PreviewDeliveryResponse{Subject: message.Subject, Text: message.Text})
}
