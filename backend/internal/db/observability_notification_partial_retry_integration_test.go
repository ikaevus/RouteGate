package db

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/delivery"
	"github.com/ikaevus/routegate/backend/internal/observability"
)

type flakySystemNotificationCreator struct {
	inner  *delivery.SystemNotificationCreator
	calls  int
	failed bool
}

func (c *flakySystemNotificationCreator) CreateSystemNotification(ctx context.Context, channel, provider, recipient, locale, idempotencyKey string) (string, error) {
	c.calls++
	if !c.failed && c.calls == 2 {
		c.failed = true
		return "", errors.New("fixture: fail second recipient once")
	}
	return c.inner.CreateSystemNotification(ctx, channel, provider, recipient, locale, idempotencyKey)
}

func TestNotificationExpansionRetriesPartialWorkWithoutDuplicateDelivery(t *testing.T) {
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

	if _, err := pool.Exec(ctx, `
		INSERT INTO delivery_recipients (channel, provider, address, display_name, enabled, metadata_json)
		VALUES
			('telegram','telegram','100100','Primary Telegram',TRUE,'{"locale":"ru"}'::jsonb),
			('email','smtp','ops@example.com','Operations Email',TRUE,'{"locale":"en"}'::jsonb)
	`); err != nil {
		t.Fatalf("create recipients: %v", err)
	}

	health := observability.NewHealthRepository(pool)
	engine := observability.NewAlertEngine(logger, pool)
	base := time.Date(2026, time.August, 14, 6, 0, 0, 0, time.UTC)
	expiresAt := base.Add(time.Hour)
	check := observability.HealthCheck{
		Key: observability.CheckHostDiskCapacity,
		Resource: observability.ResourceRef{Type: "server", ID: "partial-retry-server"},
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
		t.Fatalf("create pending: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, base.Add(observability.AlertCriticalFireDelay+time.Second)); err != nil {
		t.Fatalf("fire alert: %v", err)
	}

	creator := &flakySystemNotificationCreator{inner: delivery.NewConfiguredSystemNotificationCreator(logger, pool)}
	worker := observability.NewNotificationWorker(observability.NewNotificationRepository(pool), creator, logger)
	processed, err := worker.ProcessNext(ctx)
	if err == nil || !processed {
		t.Fatalf("first expansion should fail after partial work: processed=%v err=%v", processed, err)
	}

	var deliveries, links int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM deliveries WHERE template_key='system_notification'`).Scan(&deliveries); err != nil {
		t.Fatalf("count first-pass deliveries: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM observability_notification_deliveries`).Scan(&links); err != nil {
		t.Fatalf("count first-pass links: %v", err)
	}
	if deliveries != 1 || links != 1 {
		t.Fatalf("partial expansion deliveries/links=%d/%d, want 1/1", deliveries, links)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE observability_notification_intents
		SET claimed_at=now()-interval '2 minutes'
		WHERE expanded_at IS NULL
	`); err != nil {
		t.Fatalf("expire notification claim: %v", err)
	}
	processed, err = worker.ProcessNext(ctx)
	if err != nil || !processed {
		t.Fatalf("retry expansion: processed=%v err=%v", processed, err)
	}

	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM deliveries WHERE template_key='system_notification'`).Scan(&deliveries); err != nil {
		t.Fatalf("count retried deliveries: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM observability_notification_deliveries`).Scan(&links); err != nil {
		t.Fatalf("count retried links: %v", err)
	}
	var expanded bool
	if err := pool.QueryRow(ctx, `SELECT expanded_at IS NOT NULL FROM observability_notification_intents LIMIT 1`).Scan(&expanded); err != nil {
		t.Fatalf("read expanded state: %v", err)
	}
	if deliveries != 2 || links != 2 || !expanded {
		t.Fatalf("retried expansion deliveries/links/expanded=%d/%d/%v, want 2/2/true", deliveries, links, expanded)
	}

	var distinctKeys int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT idempotency_key)
		FROM deliveries
		WHERE template_key='system_notification'
	`).Scan(&distinctKeys); err != nil {
		t.Fatalf("count idempotency keys: %v", err)
	}
	if distinctKeys != 2 {
		t.Fatalf("distinct notification idempotency keys=%d, want 2", distinctKeys)
	}
}
