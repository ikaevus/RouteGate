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

func TestAlertNotificationIntentsFollowIncidentLifecycle(t *testing.T) {
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
	base := time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC)
	resource := observability.ResourceRef{Type: "server", ID: "intent-server"}
	expiresAt := base.Add(time.Hour)
	check := observability.HealthCheck{
		Key: observability.CheckHostDiskCapacity,
		Resource: resource,
		Component: "host",
		State: observability.HealthDegraded,
		Required: true,
		ReasonCode: "disk_free_low",
		Summary: "Root filesystem free space is low.",
		ObservedAt: base,
		ExpiresAt: &expiresAt,
	}
	if err := health.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("apply degraded health: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, base); err != nil {
		t.Fatalf("create pending alert: %v", err)
	}
	assertNotificationIntentCounts(t, ctx, pool, 0, 0, 0)

	firingAt := base.Add(observability.AlertWarningFireDelay + time.Second)
	if err := engine.EvaluateOnce(ctx, firingAt); err != nil {
		t.Fatalf("fire alert: %v", err)
	}
	assertNotificationIntentCounts(t, ctx, pool, 1, 0, 0)

	check.State = observability.HealthUnhealthy
	check.ReasonCode = "disk_free_critical"
	check.Summary = "Root filesystem free space is critically low."
	check.ObservedAt = firingAt.Add(time.Second)
	if err := health.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("apply critical health: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, check.ObservedAt); err != nil {
		t.Fatalf("escalate alert: %v", err)
	}
	assertNotificationIntentCounts(t, ctx, pool, 1, 1, 0)

	check.State = observability.HealthHealthy
	check.ReasonCode = "disk_capacity_ok"
	check.Summary = "Root filesystem capacity is healthy."
	check.ObservedAt = firingAt.Add(2 * time.Second)
	if err := health.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("apply healthy state: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, check.ObservedAt); err != nil {
		t.Fatalf("start recovery: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, check.ObservedAt.Add(observability.AlertRecoveryDelay)); err != nil {
		t.Fatalf("resolve alert: %v", err)
	}
	assertNotificationIntentCounts(t, ctx, pool, 1, 1, 1)

	var transitions, intents int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM observability_alert_transitions`).Scan(&transitions); err != nil {
		t.Fatalf("count transitions: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM observability_notification_intents`).Scan(&intents); err != nil {
		t.Fatalf("count intents: %v", err)
	}
	if intents != 3 || transitions <= intents {
		t.Fatalf("transitions=%d intents=%d; pending transition must remain notification-silent", transitions, intents)
	}
}

func TestTransientPendingAlertDoesNotCreateNotificationIntent(t *testing.T) {
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
	base := time.Date(2026, time.August, 14, 4, 0, 0, 0, time.UTC)
	expiresAt := base.Add(time.Hour)
	check := observability.HealthCheck{
		Key: observability.CheckHostDiskCapacity,
		Resource: observability.ResourceRef{Type: "server", ID: "transient-server"},
		Component: "host",
		State: observability.HealthUnhealthy,
		Required: true,
		ReasonCode: "disk_free_critical",
		Summary: "Root filesystem free space is critically low.",
		ObservedAt: base,
		ExpiresAt: &expiresAt,
	}
	if err := health.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("apply unhealthy state: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, base); err != nil {
		t.Fatalf("create pending: %v", err)
	}

	check.State = observability.HealthHealthy
	check.ReasonCode = "disk_capacity_ok"
	check.Summary = "Root filesystem capacity is healthy."
	check.ObservedAt = base.Add(10 * time.Second)
	if err := health.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("apply recovery: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, check.ObservedAt); err != nil {
		t.Fatalf("clear pending: %v", err)
	}
	assertNotificationIntentCounts(t, ctx, pool, 0, 0, 0)
}

func assertNotificationIntentCounts(t *testing.T, ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) rowScanner
}, firing, escalated, resolved int) {
	t.Helper()
	var gotFiring, gotEscalated, gotResolved int
	if err := pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE kind='firing'),
			COUNT(*) FILTER (WHERE kind='escalated'),
			COUNT(*) FILTER (WHERE kind='resolved')
		FROM observability_notification_intents
	`).Scan(&gotFiring, &gotEscalated, &gotResolved); err != nil {
		t.Fatalf("read notification intents: %v", err)
	}
	if gotFiring != firing || gotEscalated != escalated || gotResolved != resolved {
		t.Fatalf("intent counts firing/escalated/resolved=%d/%d/%d, want %d/%d/%d", gotFiring, gotEscalated, gotResolved, firing, escalated, resolved)
	}
}

type rowScanner interface {
	Scan(...any) error
}
