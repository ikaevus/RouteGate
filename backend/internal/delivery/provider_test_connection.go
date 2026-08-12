package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/smtp"
	"strings"
)

func (p *SMTPProvider) Test(ctx context.Context) ProviderResult {
	if !p.Configured() {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: p.ConfigurationErrorCode()}
	}
	client, err := p.connect(ctx)
	if err != nil {
		return classifySMTPPreDataError(err, "smtp_connect_failed", ErrorClassTransient)
	}
	defer client.Close()
	if p.config.Username != "" {
		auth := smtp.PlainAuth("", p.config.Username, p.config.Password, p.config.Host)
		if err := client.Auth(auth); err != nil {
			return classifySMTPPreDataError(err, "smtp_auth_failed", ErrorClassPermanent)
		}
	}
	_ = client.Quit()
	return ProviderResult{Outcome: OutcomeAccepted}
}

func (p *TelegramProvider) Test(ctx context.Context) ProviderResult {
	if !p.Configured() {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: p.ConfigurationErrorCode()}
	}
	requestURL := strings.TrimRight(p.apiBaseURL, "/") + "/bot" + p.botToken + "/getMe"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "telegram_request_invalid"}
	}
	req.Header.Set("Accept", "application/json")
	response, err := p.client.Do(req)
	if err != nil {
		return ProviderResult{Outcome: OutcomeRetryableFailure, ErrorClass: ErrorClassTransient, ErrorCode: "telegram_unavailable"}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return ProviderResult{Outcome: OutcomeRetryableFailure, ErrorClass: ErrorClassTransient, ErrorCode: "telegram_unavailable"}
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return ProviderResult{Outcome: OutcomeRetryableFailure, ErrorClass: ErrorClassTransient, ErrorCode: telegramHTTPErrorCode(response.StatusCode)}
	}
	if response.StatusCode >= 400 {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: telegramHTTPErrorCode(response.StatusCode)}
	}
	var decoded struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil || !decoded.OK {
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: "telegram_response_invalid"}
	}
	return ProviderResult{Outcome: OutcomeAccepted}
}
