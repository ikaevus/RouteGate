package delivery

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type PreviewDeliveryRequest struct {
	Locale   string `json:"locale"`
	Template string `json:"template"`
	Channel  string `json:"channel"`
	AttachQR bool   `json:"attachQr"`
}

type PreviewDeliveryResponse struct {
	Subject     string   `json:"subject"`
	Text        string   `json:"text"`
	HTML        string   `json:"html,omitempty"`
	Protocol    string   `json:"protocol,omitempty"`
	Attachments []string `json:"attachments,omitempty"`
}

func (h *Handler) PreviewForVPNAccount(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.PathValue("id"))

	var request PreviewDeliveryRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must be valid JSON."))
		return
	}
	request.Locale = strings.ToLower(strings.TrimSpace(request.Locale))
	request.Template = strings.ToLower(strings.TrimSpace(request.Template))
	request.Channel = strings.ToLower(strings.TrimSpace(request.Channel))
	if request.Channel == "" {
		request.Channel = "email"
	}
	if !validLocale(request.Locale) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("delivery_locale_unsupported", "Delivery locale must be en or ru."))
		return
	}
	if request.Template != TemplateVPNAccess && request.Template != TemplateVPNAccessReissued {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("delivery_template_unsupported", "This VPN access template is not supported."))
		return
	}
	if request.Channel != "email" && request.Channel != "telegram" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("delivery_channel_unsupported", "Delivery channel must be email or telegram."))
		return
	}

	resolver := h.resolver
	if resolver == nil {
		resolver = NewVPNAccessResolver(h.accounts, h.publicURL)
	}
	material, err := resolver.Resolve(r.Context(), Delivery{
		VPNAccountID: accountID,
		Channel:      request.Channel,
		TemplateKey:  request.Template,
		Locale:       request.Locale,
		AttachQR:     request.AttachQR,
	})
	if err != nil {
		failure := failureFromError(err, ErrorClassPermanent, "vpn_access_unavailable")
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error(failure.Code, "VPN access is not ready to preview yet."))
		return
	}
	message, err := NewRenderer().Render(request.Template, request.Locale, material.TemplateData)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("delivery_preview_failed", "Delivery preview could not be rendered."))
		return
	}
	attachments := make([]string, 0, len(material.Attachments))
	for _, attachment := range material.Attachments {
		if name := strings.TrimSpace(attachment.Filename); name != "" {
			attachments = append(attachments, name)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, PreviewDeliveryResponse{
		Subject:     message.Subject,
		Text:        message.Text,
		HTML:        message.HTML,
		Protocol:    material.TemplateData.Access.Protocol,
		Attachments: attachments,
	})
}
