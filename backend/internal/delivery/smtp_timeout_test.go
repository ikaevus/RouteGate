package delivery

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/config"
)

func TestSMTPProviderTestBoundsDialWithProviderTimeout(t *testing.T) {
	provider := NewSMTPProvider(config.SMTPConfig{
		Host:        "smtp.example.invalid",
		Port:        587,
		Username:    "routegate@example.invalid",
		Password:    "test-password",
		FromAddress: "routegate@example.invalid",
		TLSMode:     "starttls",
	})
	provider.timeout = 25 * time.Millisecond
	provider.dialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	started := time.Now()
	result := provider.Test(context.Background())
	elapsed := time.Since(started)

	if elapsed > 250*time.Millisecond {
		t.Fatalf("SMTP test exceeded bounded dial timeout: %v", elapsed)
	}
	if result.Outcome != OutcomeRetryableFailure || result.ErrorCode != "smtp_connect_failed" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSMTPProviderTestClassifiesImplicitTLSHandshakeFailure(t *testing.T) {
	provider := NewSMTPProvider(config.SMTPConfig{
		Host:        "smtp.example.invalid",
		Port:        465,
		Username:    "routegate@example.invalid",
		Password:    "test-password",
		FromAddress: "routegate@example.invalid",
		TLSMode:     "tls",
	})
	provider.tlsDial = func(context.Context, string, string, *tls.Config) (net.Conn, error) {
		return nil, errors.New("TLS handshake rejected")
	}

	result := provider.Test(context.Background())
	if result.Outcome != OutcomePermanentFailure || result.ErrorCode != "smtp_tls_failed" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
