package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/observability"
)

func TestAlertEngineLifecycle(t *testing.T) {
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
	now := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)
	check := observability.HealthCheck{
		Key: observability.CheckHostDiskCapacity,
		Resource: observability.ResourceRef{Type: "server", ID: "server-1"},
		Component: "host", State: observability.HealthDegraded, Required: true,
		ReasonCode: "disk_free_low", Summary: "Root filesystem free space is low.", ObservedAt: now,
	}
	if err := health.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("seed health: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, now); err != nil {
		t.Fatalf("pending: %v", err)
	}
	assertLatestDiskAlert(t, ctx, pool, "pending", "warning", 1)

	firingAt := now.Add(observability.AlertWarningFireDelay + time.Second)
	if err := engine.EvaluateOnce(ctx, firingAt); err != nil {
		t.Fatalf("firing: %v", err)
	}
	assertLatestDiskAlert(t, ctx, pool, "firing", "warning", 1)

	check.State = observability.HealthUnhealthy
	check.ReasonCode = "disk_free_critical"
	check.Summary = "Root filesystem free space is critically low."
	check.ObservedAt = firingAt.Add(time.Second)
	if err := health.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("escalate health: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, check.ObservedAt); err != nil {
		t.Fatalf("escalate alert: %v", err)
	}
	assertLatestDiskAlert(t, ctx, pool, "firing", "critical", 1)

	check.State = observability.HealthHealthy
	check.ReasonCode = "disk_capacity_ok"
	check.Summary = "Root filesystem capacity is healthy."
	check.ObservedAt = firingAt.Add(2 * time.Second)
	if err := health.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("recover health: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, check.ObservedAt); err != nil {
		t.Fatalf("start recovery: %v", err)
	}
	resolvedAt := check.ObservedAt.Add(observability.AlertRecoveryDelay)
	if err := engine.EvaluateOnce(ctx, resolvedAt); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	assertLatestDiskAlert(t, ctx, pool, "resolved", "critical", 1)

	check.State = observability.HealthUnhealthy
	check.ReasonCode = "disk_free_critical"
	check.Summary = "Root filesystem free space is critically low again."
	check.ObservedAt = resolvedAt.Add(time.Minute)
	if err := health.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("recurrent health: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, check.ObservedAt); err != nil {
		t.Fatalf("recurrent pending: %v", err)
	}
	assertLatestDiskAlert(t, ctx, pool, "pending", "critical", 2)
	if err := engine.EvaluateOnce(ctx, check.ObservedAt.Add(observability.AlertCriticalFireDelay+time.Second)); err != nil {
		t.Fatalf("flap delay: %v", err)
	}
	assertLatestDiskAlert(t, ctx, pool, "pending", "critical", 2)
	if err := engine.EvaluateOnce(ctx, check.ObservedAt.Add(observability.AlertFlapFireDelay)); err != nil {
		t.Fatalf("recurrent firing: %v", err)
	}
	assertLatestDiskAlert(t, ctx, pool, "firing", "critical", 2)
}

func assertLatestDiskAlert(t *testing.T, ctx context.Context, pool *pgxpool.Pool, state, severity string, episodes int) {
	t.Helper()
	var gotState, gotSeverity string
	var count int
	if err := pool.QueryRow(ctx, "SELECT condition_state, severity, (SELECT COUNT(*) FROM observability_alerts WHERE fingerprint='host.disk.capacity:server:server-1') FROM observability_alerts WHERE fingerprint='host.disk.capacity:server:server-1' ORDER BY started_at DESC, id DESC LIMIT 1").Scan(&gotState, &gotSeverity, &count); err != nil {
		t.Fatalf("read alert: %v", err)
	}
	if gotState != state || gotSeverity != severity || count != episodes {
		t.Fatalf("alert=%s/%s episodes=%d want=%s/%s/%d", gotState, gotSeverity, count, state, severity, episodes)
	}
}
