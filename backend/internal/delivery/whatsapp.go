package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/ikaevus/routegate/backend/internal/config"
)

const whatsappGraphBaseURL = "https://graph.facebook.com"

type WhatsAppProvider struct {
	config      config.WhatsAppConfig
	configError string
	apiBaseURL  string
	client      *http.Client
}

type whatsappTemplateRequest struct {
	MessagingProduct string                   `json:"messaging_product"`
	RecipientType    string                   `json:"recipient_type"`
	To               string                   `json:"to"`
	Type             string                   `json:"type"`
	Template         whatsappTemplateObject   `json:"template"`
}

type whatsappTemplateObject struct {
	Name       string                      `json:"name"`
	Language   whatsappLanguageObject      `json:"language"`
	Components []whatsappTemplateComponent `json:"components"`
}

type whatsappLanguageObject struct {
	Code string `json:"code"`
}

type whatsappTemplateComponent struct {
	Type       string                      `json:"type"`
	Parameters []whatsappTemplateParameter `json:"parameters"`
}

type whatsappTemplateParameter struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type whatsappAPIResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}

func NewWhatsAppProvider(cfg config.WhatsAppConfig) *WhatsAppProvider {
	provider := &WhatsAppProvider{
		config:     normalizeWhatsAppConfig(cfg),
		apiBaseURL: whatsappGraphBaseURL,
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	provider.configError = validateWhatsAppConfig(provider.config)
	return provider
}

func (p *WhatsAppProvider) Name() string    { return "whatsapp" }
func (p *WhatsAppProvider) Channel() string { return "whatsapp" }
func (p *WhatsAppProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{HTML: false, Attachments: false, DeliveryReceipts: false}
}
func (p *WhatsAppProvider) Configured() bool {
	return p != nil && p.configError == ""
}
func (p *WhatsAppProvider) ConfigurationErrorCode() string {
	if p == nil {
		return "whatsapp_not_configured"
	}
	return p.configError
}

func (p *WhatsAppProvider) Send(ctx context.Context, message Message) ProviderResult {
	if !p.Configured() {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: p.ConfigurationErrorCode()}
	}
	recipient, err := normalizeWhatsAppRecipient(message.Recipient)
	if err != nil {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "whatsapp_invalid_recipient"}
	}
	templateName, languageCode, ok := p.templateFor(message.TemplateKey, message.Locale)
	if !ok {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "whatsapp_template_unsupported"}
	}
	actionURL := strings.TrimSpace(message.ActionURL)
	if actionURL == "" {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "access_url_missing"}
	}

	payload, err := json.Marshal(whatsappTemplateRequest{
		MessagingProduct: "whatsapp",
		RecipientType:    "individual",
		To:               recipient,
		Type:             "template",
		Template: whatsappTemplateObject{
			Name:     templateName,
			Language: whatsappLanguageObject{Code: languageCode},
			Components: []whatsappTemplateComponent{{
				Type: "body",
				Parameters: []whatsappTemplateParameter{{
					Type: "text",
					Text: actionURL,
				}},
			}},
		},
	})
	if err != nil {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "message_encode_failed"}
	}

	requestURL := strings.TrimRight(p.apiBaseURL, "/") + "/" + p.config.GraphAPIVersion + "/" + p.config.PhoneNumberID + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(payload))
	if err != nil {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "whatsapp_request_invalid"}
	}
	req.Header.Set("Authorization", "Bearer "+p.config.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	response, err := p.client.Do(req)
	if err != nil {
		// Once the request has entered the HTTP transport, RouteGate cannot prove
		// whether Meta accepted it. Preserve the no-duplicate invariant.
		return ProviderResult{Outcome: OutcomeUncertain, ErrorClass: ErrorClassUncertain, ErrorCode: "whatsapp_request_uncertain"}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return ProviderResult{Outcome: OutcomeUncertain, ErrorClass: ErrorClassUncertain, ErrorCode: "whatsapp_response_uncertain"}
	}

	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return ProviderResult{Outcome: OutcomeRetryableFailure, ErrorClass: ErrorClassTransient, ErrorCode: "whatsapp_rate_limited"}
	case response.StatusCode >= 500:
		return ProviderResult{Outcome: OutcomeRetryableFailure, ErrorClass: ErrorClassTransient, ErrorCode: "whatsapp_server_error"}
	case response.StatusCode == http.StatusUnauthorized:
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "whatsapp_unauthorized"}
	case response.StatusCode == http.StatusForbidden:
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "whatsapp_forbidden"}
	case response.StatusCode == http.StatusBadRequest:
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "whatsapp_bad_request"}
	case response.StatusCode == http.StatusNotFound:
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "whatsapp_not_found"}
	case response.StatusCode >= 300 && response.StatusCode < 400:
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "whatsapp_redirect_rejected"}
	case response.StatusCode >= 400:
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "whatsapp_request_rejected"}
	}

	var apiResponse whatsappAPIResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil || len(apiResponse.Messages) == 0 {
		return ProviderResult{Outcome: OutcomeUncertain, ErrorClass: ErrorClassUncertain, ErrorCode: "whatsapp_response_uncertain"}
	}
	messageID := strings.TrimSpace(apiResponse.Messages[0].ID)
	if messageID == "" {
		return ProviderResult{Outcome: OutcomeUncertain, ErrorClass: ErrorClassUncertain, ErrorCode: "whatsapp_response_uncertain"}
	}
	return ProviderResult{Outcome: OutcomeAccepted, ProviderReference: messageID}
}

func (p *WhatsAppProvider) templateFor(templateKey, locale string) (string, string, bool) {
	if p == nil {
		return "", "", false
	}
	var templateName string
	switch strings.ToLower(strings.TrimSpace(templateKey)) {
	case TemplateVPNAccess:
		templateName = p.config.VPNAccessTemplate
	case TemplateVPNAccessReissued:
		templateName = p.config.VPNAccessReissuedTemplate
	default:
		return "", "", false
	}
	var languageCode string
	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "en":
		languageCode = p.config.LanguageEN
	case "ru":
		languageCode = p.config.LanguageRU
	default:
		return "", "", false
	}
	return templateName, languageCode, templateName != "" && languageCode != ""
}

func normalizeWhatsAppConfig(cfg config.WhatsAppConfig) config.WhatsAppConfig {
	cfg.AccessToken = strings.TrimSpace(cfg.AccessToken)
	cfg.PhoneNumberID = strings.TrimSpace(cfg.PhoneNumberID)
	cfg.GraphAPIVersion = strings.TrimSpace(cfg.GraphAPIVersion)
	cfg.VPNAccessTemplate = strings.TrimSpace(cfg.VPNAccessTemplate)
	cfg.VPNAccessReissuedTemplate = strings.TrimSpace(cfg.VPNAccessReissuedTemplate)
	cfg.LanguageEN = strings.TrimSpace(cfg.LanguageEN)
	cfg.LanguageRU = strings.TrimSpace(cfg.LanguageRU)
	return cfg
}

func validateWhatsAppConfig(cfg config.WhatsAppConfig) string {
	if cfg.AccessToken == "" && cfg.PhoneNumberID == "" && cfg.GraphAPIVersion == "" && cfg.VPNAccessTemplate == "" && cfg.VPNAccessReissuedTemplate == "" {
		return "whatsapp_not_configured"
	}
	if cfg.AccessToken == "" || !digitsOnly(cfg.PhoneNumberID) || !validGraphAPIVersion(cfg.GraphAPIVersion) || !validWhatsAppTemplateName(cfg.VPNAccessTemplate) || !validWhatsAppTemplateName(cfg.VPNAccessReissuedTemplate) || !validWhatsAppLanguageCode(cfg.LanguageEN) || !validWhatsAppLanguageCode(cfg.LanguageRU) {
		return "whatsapp_configuration_invalid"
	}
	return ""
}

func validGraphAPIVersion(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 4 || value[0] != 'v' {
		return false
	}
	seenDot := false
	for _, char := range value[1:] {
		if char == '.' && !seenDot {
			seenDot = true
			continue
		}
		if char < '0' || char > '9' {
			return false
		}
	}
	return seenDot && !strings.HasSuffix(value, ".")
}

func validWhatsAppTemplateName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' {
			continue
		}
		return false
	}
	return true
}

func validWhatsAppLanguageCode(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 16 {
		return false
	}
	for _, char := range value {
		if unicode.IsLetter(char) || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func normalizeWhatsAppRecipient(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", Failure{Class: ErrorClassPermanent, Code: "whatsapp_invalid_recipient"}
	}
	var digits strings.Builder
	for index, char := range value {
		if char >= '0' && char <= '9' {
			digits.WriteRune(char)
			continue
		}
		if char == '+' && index == 0 {
			continue
		}
		if char == ' ' || char == '-' || char == '(' || char == ')' || char == '.' {
			continue
		}
		return "", Failure{Class: ErrorClassPermanent, Code: "whatsapp_invalid_recipient"}
	}
	normalized := digits.String()
	if len(normalized) < 8 || len(normalized) > 15 || normalized[0] == '0' {
		return "", Failure{Class: ErrorClassPermanent, Code: "whatsapp_invalid_recipient"}
	}
	return normalized, nil
}

func digitsOnly(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
