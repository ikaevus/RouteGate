package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ikaevus/routegate/backend/internal/config"
)

const telegramAPIBaseURL = "https://api.telegram.org"

type TelegramProvider struct {
	botToken    string
	configError string
	apiBaseURL  string
	client      *http.Client
}

type telegramSendMessageRequest struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

type telegramAPIResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		MessageID int64 `json:"message_id"`
	} `json:"result"`
}

func NewTelegramProvider(cfg config.TelegramConfig) *TelegramProvider {
	token := strings.TrimSpace(cfg.BotToken)
	provider := &TelegramProvider{
		botToken:   token,
		apiBaseURL: telegramAPIBaseURL,
		client:     &http.Client{Timeout: 15 * time.Second},
	}
	provider.configError = validateTelegramBotToken(token)
	return provider
}

func (p *TelegramProvider) Name() string    { return "telegram" }
func (p *TelegramProvider) Channel() string { return "telegram" }
func (p *TelegramProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{HTML: false, Attachments: false, DeliveryReceipts: false}
}
func (p *TelegramProvider) Configured() bool {
	return p != nil && p.configError == ""
}
func (p *TelegramProvider) ConfigurationErrorCode() string {
	if p == nil {
		return "telegram_not_configured"
	}
	return p.configError
}

func (p *TelegramProvider) Send(ctx context.Context, message Message) ProviderResult {
	if !p.Configured() {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: p.ConfigurationErrorCode()}
	}
	chatID, err := normalizeTelegramChatID(message.Recipient)
	if err != nil {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "telegram_invalid_chat_id"}
	}
	text := strings.TrimSpace(message.Text)
	if text == "" {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "message_body_missing"}
	}
	if len([]rune(text)) > 4096 {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "telegram_message_too_long"}
	}
	payload, err := json.Marshal(telegramSendMessageRequest{ChatID: chatID, Text: text})
	if err != nil {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "message_encode_failed"}
	}

	requestURL := strings.TrimRight(p.apiBaseURL, "/") + "/bot" + p.botToken + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(payload))
	if err != nil {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "telegram_request_invalid"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	response, err := p.client.Do(req)
	if err != nil {
		// Once the HTTP client begins a request, RouteGate cannot prove whether Telegram
		// received it. Preserve the no-duplicate invariant by treating network errors as uncertain.
		return ProviderResult{Outcome: OutcomeUncertain, ErrorClass: ErrorClassUncertain, ErrorCode: "telegram_request_uncertain"}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return ProviderResult{Outcome: OutcomeUncertain, ErrorClass: ErrorClassUncertain, ErrorCode: "telegram_response_uncertain"}
	}

	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return ProviderResult{Outcome: OutcomeRetryableFailure, ErrorClass: ErrorClassTransient, ErrorCode: telegramHTTPErrorCode(response.StatusCode)}
	}
	if response.StatusCode >= 400 {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: telegramHTTPErrorCode(response.StatusCode)}
	}

	var decoded telegramAPIResponse
	if err := json.Unmarshal(body, &decoded); err != nil || !decoded.OK || decoded.Result.MessageID == 0 {
		return ProviderResult{Outcome: OutcomeUncertain, ErrorClass: ErrorClassUncertain, ErrorCode: "telegram_response_uncertain"}
	}
	return ProviderResult{
		Outcome:           OutcomeAccepted,
		ProviderReference: "message:" + strconv.FormatInt(decoded.Result.MessageID, 10),
	}
}

func validateTelegramBotToken(token string) string {
	if token == "" {
		return "telegram_not_configured"
	}
	if len(token) < 20 || len(token) > 200 || strings.ContainsAny(token, " \t\r\n/\\") {
		return "telegram_configuration_invalid"
	}
	parts := strings.Split(token, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "telegram_configuration_invalid"
	}
	if _, err := strconv.ParseInt(parts[0], 10, 64); err != nil {
		return "telegram_configuration_invalid"
	}
	return ""
}

func normalizeTelegramChatID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 32 || strings.ContainsAny(value, " \t\r\n") {
		return "", fmt.Errorf("invalid chat id")
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed == 0 {
		return "", fmt.Errorf("invalid chat id")
	}
	return strconv.FormatInt(parsed, 10), nil
}

func telegramHTTPErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "telegram_bad_request"
	case http.StatusUnauthorized:
		return "telegram_unauthorized"
	case http.StatusForbidden:
		return "telegram_forbidden"
	case http.StatusNotFound:
		return "telegram_not_found"
	case http.StatusTooManyRequests:
		return "telegram_rate_limited"
	default:
		if status >= 500 {
			return "telegram_unavailable"
		}
		return "telegram_request_failed"
	}
}
