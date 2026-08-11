package delivery

import (
	"bytes"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/config"
)

func TestBuildConnectURLKeepsCredentialInFragment(t *testing.T) {
	vless := "vless://00000000-0000-0000-0000-000000000000@example.invalid:8443?security=reality#RouteGate"
	connectURL, err := BuildConnectURL("https://vpn.example.com/", vless)
	if err != nil {
		t.Fatalf("build connect URL: %v", err)
	}
	parsed, err := url.Parse(connectURL)
	if err != nil {
		t.Fatalf("parse connect URL: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "vpn.example.com" || parsed.Path != "/connect.html" {
		t.Fatalf("unexpected connect URL: %q", connectURL)
	}
	if parsed.RawQuery != "" || strings.Contains(parsed.Path, "vless") || strings.Contains(parsed.RawQuery, "00000000") {
		t.Fatalf("access material escaped into request URL: %q", connectURL)
	}
	if !strings.HasPrefix(parsed.Fragment, "vless=") {
		t.Fatalf("missing access fragment: %q", parsed.Fragment)
	}
}

func TestNormalizePublicURLRejectsUnsafeForms(t *testing.T) {
	for _, value := range []string{
		"http://vpn.example.com",
		"https://user:pass@vpn.example.com",
		"https://vpn.example.com/path",
		"https://vpn.example.com?x=1",
		"https://vpn.example.com/#fragment",
	} {
		if _, err := NormalizePublicURL(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestRendererEscapesDynamicHTML(t *testing.T) {
	message, err := NewRenderer().Render(TemplateVPNAccess, "en", TemplateData{
		ProfileName: `<script>alert("x")</script>`,
		ConnectURL:  "https://vpn.example.com/connect.html#fixture",
	})
	if err != nil {
		t.Fatalf("render template: %v", err)
	}
	if message.HTML == "" {
		t.Fatal("expected HTML alternative")
	}
	if strings.Contains(message.HTML, "<script>") || !strings.Contains(message.HTML, "&lt;script&gt;") {
		t.Fatalf("dynamic HTML was not escaped: %q", message.HTML)
	}
}

func TestRenderQRCodePNGIsInMemoryPNG(t *testing.T) {
	pngBytes, err := RenderQRCodePNG("vless://fixture@example.invalid:8443")
	if err != nil {
		t.Fatalf("render QR: %v", err)
	}
	if len(pngBytes) < 8 || !bytes.Equal(pngBytes[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		t.Fatalf("unexpected PNG signature: %x", pngBytes[:min(8, len(pngBytes))])
	}
}

func TestBuildMIMEMessageRejectsHeaderInjection(t *testing.T) {
	_, _, err := buildMIMEMessage(Message{
		Recipient: "user@example.invalid",
		Subject:   "RouteGate\r\nBcc: attacker@example.invalid",
		Text:      "fixture",
	}, "routegate@example.invalid", "RouteGate", time.Now())
	if err == nil {
		t.Fatal("expected subject header injection to be rejected")
	}
}

func TestBuildMIMEMessagePlainTextHasTransferEncoding(t *testing.T) {
	raw, recipient, err := buildMIMEMessage(Message{
		Recipient: "user@example.invalid",
		Subject:   "RouteGate access",
		Text:      "Привет RouteGate",
	}, "routegate@example.invalid", "RouteGate", time.Date(2026, 8, 11, 20, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("build MIME: %v", err)
	}
	if recipient != "user@example.invalid" {
		t.Fatalf("recipient=%q", recipient)
	}
	text := string(raw)
	if !strings.Contains(text, "Content-Transfer-Encoding: quoted-printable\r\n") {
		t.Fatalf("missing quoted-printable header: %q", text)
	}
}

func TestBuildMIMEMessageContainsAlternativeAndAttachment(t *testing.T) {
	raw, _, err := buildMIMEMessage(Message{
		Recipient: "user@example.invalid",
		Subject:   "RouteGate access",
		Text:      "Open VPN",
		HTML:      "<p>Open VPN</p>",
		Attachments: []Attachment{{
			Filename:    "routegate-vpn-qr.png",
			ContentType: "image/png",
			Content:     []byte("png-fixture"),
		}},
	}, "routegate@example.invalid", "RouteGate", time.Now())
	if err != nil {
		t.Fatalf("build MIME: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "multipart/mixed") || !strings.Contains(text, "multipart/alternative") || !strings.Contains(text, "routegate-vpn-qr.png") {
		t.Fatalf("expected multipart message with attachment: %q", text)
	}
}

func TestSMTPProviderNormalizesTLSModeAndReportsCapabilities(t *testing.T) {
	provider := NewSMTPProvider(config.SMTPConfig{
		Host:        "smtp.example.invalid",
		Port:        465,
		FromAddress: "routegate@example.invalid",
		FromName:    "RouteGate",
		TLSMode:     " TLS ",
	})
	if !provider.Configured() || provider.config.TLSMode != smtpTLSModeTLS {
		t.Fatalf("provider was not normalized: configured=%v mode=%q code=%q", provider.Configured(), provider.config.TLSMode, provider.ConfigurationErrorCode())
	}
	caps := provider.Capabilities()
	if !caps.HTML || !caps.Attachments || caps.DeliveryReceipts {
		t.Fatalf("unexpected SMTP capabilities: %+v", caps)
	}
}

func TestSMTPProviderRejectsPlaintextMode(t *testing.T) {
	provider := NewSMTPProvider(config.SMTPConfig{
		Host:        "smtp.example.invalid",
		Port:        25,
		FromAddress: "routegate@example.invalid",
		TLSMode:     "none",
	})
	if provider.Configured() || provider.ConfigurationErrorCode() != "smtp_configuration_invalid" {
		t.Fatalf("plaintext SMTP unexpectedly accepted: configured=%v code=%q", provider.Configured(), provider.ConfigurationErrorCode())
	}
}

func TestSMTPResponseClassificationUsesSafeStatusClasses(t *testing.T) {
	transient := classifySMTPPreDataError(&textproto.Error{Code: 451, Msg: "temporary provider detail that must not persist"}, "smtp_data_rejected", ErrorClassPermanent)
	if transient.Outcome != OutcomeRetryableFailure || transient.ErrorCode != "smtp_data_rejected" {
		t.Fatalf("unexpected 451 classification: %+v", transient)
	}
	permanent := classifySMTPPreDataError(&textproto.Error{Code: 550, Msg: "private provider detail"}, "invalid_recipient", ErrorClassTransient)
	if permanent.Outcome != OutcomePermanentFailure || permanent.ErrorCode != "invalid_recipient" {
		t.Fatalf("unexpected 550 classification: %+v", permanent)
	}
}

func TestProviderInfoNeverContainsSMTPCredentials(t *testing.T) {
	provider := NewSMTPProvider(config.SMTPConfig{
		Host:        "smtp.example.invalid",
		Port:        587,
		Username:    "routegate-user",
		Password:    "provider-secret-fixture",
		FromAddress: "routegate@example.invalid",
		TLSMode:     "starttls",
	})
	registry, err := NewRegistry(provider)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	items := registry.Info()
	if len(items) != 1 || !items[0].Configured {
		t.Fatalf("unexpected provider info: %+v", items)
	}
	serialized := strings.ToLower(strings.TrimSpace(items[0].ConfigurationError + items[0].Name + items[0].Channel))
	if strings.Contains(serialized, "routegate-user") || strings.Contains(serialized, "provider-secret") {
		t.Fatalf("provider credentials leaked into info: %+v", items[0])
	}
}
