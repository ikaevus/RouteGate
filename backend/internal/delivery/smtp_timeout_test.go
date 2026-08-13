package delivery

import (
	"context"
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
