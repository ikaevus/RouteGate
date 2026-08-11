package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/config"
)

func validWhatsAppTestConfig() config.WhatsAppConfig {
	return config.WhatsAppConfig{
		AccessToken:               "test-access-token",
		PhoneNumberID:             "1234567890",
		GraphAPIVersion:           "v23.0",
		VPNAccessTemplate:         "routegate_vpn_access",
		VPNAccessReissuedTemplate: "routegate_vpn_access_reissued",
		LanguageEN:                "en_US",
		LanguageRU:                "ru",
	}
}

func TestNormalizeWhatsAppRecipient(t *testing.T) {
	for input, want := range map[string]string{
		"+1 (555) 123-4567": "15551234567",
		"447700900123":      "447700900123",
		"+49 151 23456789":   "4915123456789",
	} {
		got, err := normalizeWhatsAppRecipient(input)
		if err != nil || got != want {
			t.Fatalf("normalize %q = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", "0123456789", "+1234", "+1-555-CALL-NOW", "1234567890123456"} {
		if got, err := normalizeWhatsAppRecipient(input); err == nil {
			t.Fatalf("normalize invalid %q = %q, want error", input, got)
		}
	}
}

func TestWhatsAppProviderSendsApprovedTemplateWithSecureLink(t *testing.T) {
	var captured whatsappTemplateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v23.0/1234567890/messages" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-access-token" {
			t.Fatalf("authorization=%q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messaging_product":"whatsapp","messages":[{"id":"wamid.test-123"}]}`))
	}))
	defer server.Close()

	provider := NewWhatsAppProvider(validWhatsAppTestConfig())
	provider.apiBaseURL = server.URL
	provider.client = server.Client()
	result := provider.Send(context.Background(), Message{
		Recipient:   "+1 (555) 123-4567",
		TemplateKey: TemplateVPNAccess,
		Locale:      "en",
		ActionURL:   "https://routegate.example/connect.html#secure-fixture",
	})
	if result.Outcome != OutcomeAccepted || result.ProviderReference != "wamid.test-123" {
		t.Fatalf("result=%+v", result)
	}
	if captured.MessagingProduct != "whatsapp" || captured.RecipientType != "individual" || captured.To != "15551234567" || captured.Type != "template" {
		t.Fatalf("request envelope=%+v", captured)
	}
	if captured.Template.Name != "routegate_vpn_access" || captured.Template.Language.Code != "en_US" {
		t.Fatalf("template=%+v", captured.Template)
	}
	if len(captured.Template.Components) != 1 || len(captured.Template.Components[0].Parameters) != 1 || captured.Template.Components[0].Parameters[0].Text != "https://routegate.example/connect.html#secure-fixture" {
		t.Fatalf("template components=%+v", captured.Template.Components)
	}
}

func TestWhatsAppProviderMapsSafeOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		want       Outcome
		wantCode   string
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{}`, want: OutcomeRetryableFailure, wantCode: "whatsapp_rate_limited"},
		{name: "server error", status: http.StatusBadGateway, body: `{}`, want: OutcomeRetryableFailure, wantCode: "whatsapp_server_error"},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{}`, want: OutcomePermanentFailure, wantCode: "whatsapp_unauthorized"},
		{name: "forbidden", status: http.StatusForbidden, body: `{}`, want: OutcomePermanentFailure, wantCode: "whatsapp_forbidden"},
		{name: "bad request", status: http.StatusBadRequest, body: `{}`, want: OutcomePermanentFailure, wantCode: "whatsapp_bad_request"},
		{name: "ambiguous success", status: http.StatusOK, body: `{}`, want: OutcomeUncertain, wantCode: "whatsapp_response_uncertain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			provider := NewWhatsAppProvider(validWhatsAppTestConfig())
			provider.apiBaseURL = server.URL
			provider.client = server.Client()
			result := provider.Send(context.Background(), Message{Recipient: "+15551234567", TemplateKey: TemplateVPNAccess, Locale: "en", ActionURL: "https://example.invalid/connect#fixture"})
			if result.Outcome != tc.want || result.ErrorCode != tc.wantCode {
				t.Fatalf("result=%+v want outcome=%q code=%q", result, tc.want, tc.wantCode)
			}
		})
	}
}

func TestWhatsAppProviderConfigurationIsExplicit(t *testing.T) {
	if got := NewWhatsAppProvider(config.WhatsAppConfig{}).ConfigurationErrorCode(); got != "whatsapp_not_configured" {
		t.Fatalf("empty config code=%q", got)
	}
	invalid := validWhatsAppTestConfig()
	invalid.GraphAPIVersion = "latest"
	if got := NewWhatsAppProvider(invalid).ConfigurationErrorCode(); got != "whatsapp_configuration_invalid" {
		t.Fatalf("invalid config code=%q", got)
	}
}

func TestWorkerPassesWhatsAppTemplateMetadataOnlyInMemory(t *testing.T) {
	delivery := queuedFixture(1, 5)
	delivery.Channel = "whatsapp"
	delivery.Provider = "capture"
	delivery.Recipient = "15551234567"
	delivery.TemplateKey = TemplateVPNAccessReissued
	delivery.Locale = "ru"
	repository := &fakeWorkerRepository{next: delivery}
	provider := &fakeProvider{name: "capture", channel: "whatsapp", result: ProviderResult{Outcome: OutcomeAccepted, ProviderReference: "wamid.fixture"}}
	registry, _ := NewRegistry(provider)
	worker := NewWorker(repository, fakeResolver{material: ResolvedMaterial{TemplateData: TemplateData{ConnectURL: "https://example.invalid/connect.html#fixture"}}}, NewRenderer(), registry, nil, nil)

	processed, err := worker.ProcessNext(context.Background())
	if err != nil || !processed || len(provider.messages) != 1 {
		t.Fatalf("process=%v err=%v messages=%+v", processed, err, provider.messages)
	}
	message := provider.messages[0]
	if message.TemplateKey != TemplateVPNAccessReissued || message.Locale != "ru" || message.ActionURL != "https://example.invalid/connect.html#fixture" {
		t.Fatalf("provider metadata=%+v", message)
	}
}
