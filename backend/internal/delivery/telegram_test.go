package delivery

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/config"
)

const testTelegramBotToken = "123456789:TESTTOKENabcdefghijklmno"

func TestTelegramProviderConfigurationAndCapabilities(t *testing.T) {
	unconfigured := NewTelegramProvider(config.TelegramConfig{})
	if unconfigured.Configured() || unconfigured.ConfigurationErrorCode() != "telegram_not_configured" {
		t.Fatalf("unexpected unconfigured provider: configured=%v code=%q", unconfigured.Configured(), unconfigured.ConfigurationErrorCode())
	}

	provider := NewTelegramProvider(config.TelegramConfig{BotToken: testTelegramBotToken})
	if !provider.Configured() {
		t.Fatalf("configured provider rejected: %q", provider.ConfigurationErrorCode())
	}
	caps := provider.Capabilities()
	if caps.HTML || caps.Attachments || caps.DeliveryReceipts {
		t.Fatalf("unexpected Telegram capabilities: %+v", caps)
	}
}

func TestTelegramProviderSendMessageSuccess(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	}))
	defer server.Close()

	provider := NewTelegramProvider(config.TelegramConfig{BotToken: testTelegramBotToken})
	provider.apiBaseURL = server.URL
	provider.client = server.Client()
	result := provider.Send(context.Background(), Message{Recipient: "123456789", Text: "RouteGate fixture"})

	if result.Outcome != OutcomeAccepted || result.ProviderReference != "message:42" {
		t.Fatalf("unexpected Telegram result: %+v", result)
	}
	if !strings.Contains(receivedBody, `"chat_id":"123456789"`) || !strings.Contains(receivedBody, `"text":"RouteGate fixture"`) {
		t.Fatalf("unexpected request body: %s", receivedBody)
	}
}

func TestTelegramProviderClassifiesKnownHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		status  int
		outcome Outcome
		code    string
	}{
		{http.StatusTooManyRequests, OutcomeRetryableFailure, "telegram_rate_limited"},
		{http.StatusServiceUnavailable, OutcomeRetryableFailure, "telegram_unavailable"},
		{http.StatusForbidden, OutcomePermanentFailure, "telegram_forbidden"},
		{http.StatusUnauthorized, OutcomePermanentFailure, "telegram_unauthorized"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"ok":false,"description":"provider detail not persisted"}`))
			}))
			defer server.Close()

			provider := NewTelegramProvider(config.TelegramConfig{BotToken: testTelegramBotToken})
			provider.apiBaseURL = server.URL
			provider.client = server.Client()
			result := provider.Send(context.Background(), Message{Recipient: "123456789", Text: "fixture"})
			if result.Outcome != tc.outcome || result.ErrorCode != tc.code {
				t.Fatalf("status %d result=%+v", tc.status, result)
			}
		})
	}
}

func TestTelegramProviderMalformedSuccessIsUncertain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()

	provider := NewTelegramProvider(config.TelegramConfig{BotToken: testTelegramBotToken})
	provider.apiBaseURL = server.URL
	provider.client = server.Client()
	result := provider.Send(context.Background(), Message{Recipient: "123456789", Text: "fixture"})
	if result.Outcome != OutcomeUncertain || result.ErrorCode != "telegram_response_uncertain" {
		t.Fatalf("unexpected malformed-response result: %+v", result)
	}
}

func TestTelegramProviderRejectsInvalidChatIDAndLongMessage(t *testing.T) {
	provider := NewTelegramProvider(config.TelegramConfig{BotToken: testTelegramBotToken})
	result := provider.Send(context.Background(), Message{Recipient: "@username", Text: "fixture"})
	if result.Outcome != OutcomePermanentFailure || result.ErrorCode != "telegram_invalid_chat_id" {
		t.Fatalf("username recipient unexpectedly accepted: %+v", result)
	}
	result = provider.Send(context.Background(), Message{Recipient: "123456789", Text: strings.Repeat("x", 4097)})
	if result.Outcome != OutcomePermanentFailure || result.ErrorCode != "telegram_message_too_long" {
		t.Fatalf("long Telegram message unexpectedly accepted: %+v", result)
	}
}

func TestTelegramChatIDNormalization(t *testing.T) {
	for _, value := range []string{"123456789", "-1001234567890"} {
		got, err := normalizeTelegramChatID(value)
		if err != nil || got != value {
			t.Fatalf("normalize %q => %q, %v", value, got, err)
		}
	}
	for _, value := range []string{"0", "", "@user", "123 456"} {
		if _, err := normalizeTelegramChatID(value); err == nil {
			t.Fatalf("invalid chat ID accepted: %q", value)
		}
	}
}
