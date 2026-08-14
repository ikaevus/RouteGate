package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/observability"
)

type countingNotificationCreator struct {
	calls int
}

func (c *countingNotificationCreator) CreateSystemNotification(context.Context, string, string, string, string, string) (string, error) {
	c.calls++
	return "", nil
}

func TestNotificationIntentWithoutRecipientsIsClosedWithoutHistoricalFlood(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := Connect(ctx, databaseURL, logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	resetPublicSchema(t, ctx, pool)
	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	health := observability.NewHealthRepository(pool)
	engine := observability.NewAlertEngine(logger, pool)
	base := time.Date(2026, time.August, 14, 5, 0, 0, 0, time.UTC)
	expiresAt := base.Add(time.Hour)
	check := observability.HealthCheck{
		Key: observability.CheckHostDiskCapacity,
		Resource: observability.ResourceRef{Type: "server", ID: "no-recipient-server"},
		Component: "host",
		State: observability.HealthUnhealthy,
		Required: true,
		ReasonCode: "disk_free_critical",
		Summary: "Root filesystem free space is critically low.",
		ObservedAt: base,
		ExpiresAt: &expiresAt,
	}
	if err := health.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("apply health: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, base); err != nil {
		t.Fatalf("create pending alert: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, base.Add(observability.AlertCriticalFireDelay+time.Second)); err != nil {
		t.Fatalf("fire alert: %v", err)
	}

	creator := &countingNotificationCreator{}
	worker := observability.NewNotificationWorker(observability.NewNotificationRepository(pool), creator, logger)
	processed, err := worker.ProcessNext(ctx)
	if err != nil || !processed {
		t.Fatalf("process intent: processed=%v err=%v", processed, err)
	}
	if creator.calls != 0 {
		t.Fatalf("creator calls = %d, want 0 with no recipients", creator.calls)
	}

	var expanded bool
	var deliveryCount int
	if err := pool.QueryRow(ctx, `
		SELECT expanded_at IS NOT NULL
		FROM observability_notification_intents
		LIMIT 1
	`).Scan(&expanded); err != nil {
		t.Fatalf("read intent expansion: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM observability_notification_deliveries`).Scan(&deliveryCount); err != nil {
		t.Fatalf("count notification deliveries: %v", err)
	}
	if !expanded || deliveryCount != 0 {
		t.Fatalf("expanded=%v deliveries=%d, want true/0", expanded, deliveryCount)
	}

	processed, err = worker.ProcessNext(ctx)
	if err != nil || processed {
		t.Fatalf("reprocess closed intent: processed=%v err=%v", processed, err)
	}
}
