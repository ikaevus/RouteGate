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

func TestAlertEngineLifecycleRecoveryAndFlapProtection(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := Connect(ctx, databaseURL, logger)
	if err != nil {
		t.Fatalf("connect to test PostgreSQL: %v", err)
	}
	defer pool.Close()

	resetPublicSchema(t, ctx, pool)
	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	healthRepository := observability.NewHealthRepository(pool)
	engine := observability.NewAlertEngine(logger, pool)
	base := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	resource := observability.ResourceRef{Type: "server", ID: "alert-server-1"}

	applyAlertTestHealth(t, ctx, healthRepository, resource, observability.HealthUnhealthy, "disk_free_critical", base)
	if err := engine.EvaluateOnce(ctx, base); err != nil {
		t.Fatalf("create pending alert: %v", err)
	}
	firstID, state, severity, recoveryStarted := readActiveAlert(t, ctx, pool, resource.ID)
	if state != "pending" || severity != "critical" || recoveryStarted != nil {
		t.Fatalf("initial alert = state=%s severity=%s recovery=%v", state, severity, recoveryStarted)
	}

	if err := engine.EvaluateOnce(ctx, base.Add(29*time.Second)); err != nil {
		t.Fatalf("evaluate pending alert: %v", err)
	}
	_, state, _, _ = readActiveAlert(t, ctx, pool, resource.ID)
	if state != "pending" {
		t.Fatalf("alert fired too early: %s", state)
	}

	if err := engine.EvaluateOnce(ctx, base.Add(31*time.Second)); err != nil {
		t.Fatalf("fire alert: %v", err)
	}
	_, state, _, _ = readActiveAlert(t, ctx, pool, resource.ID)
	if state != "firing" {
		t.Fatalf("alert state = %s, want firing", state)
	}

	recoveryStart := base.Add(40 * time.Second)
	applyAlertTestHealth(t, ctx, healthRepository, resource, observability.HealthHealthy, "disk_capacity_ok", recoveryStart)
	if err := engine.EvaluateOnce(ctx, recoveryStart); err != nil {
		t.Fatalf("start alert recovery: %v", err)
	}
	_, state, _, recoveryStarted = readActiveAlert(t, ctx, pool, resource.ID)
	if state != "firing" || recoveryStarted == nil || !recoveryStarted.Equal(recoveryStart) {
		t.Fatalf("recovery did not start correctly: state=%s recovery=%v", state, recoveryStarted)
	}

	if err := engine.EvaluateOnce(ctx, recoveryStart.Add(observability.AlertRecoveryDelay-time.Second)); err != nil {
		t.Fatalf("evaluate recovery window: %v", err)
	}
	_, state, _, _ = readActiveAlert(t, ctx, pool, resource.ID)
	if state != "firing" {
		t.Fatalf("alert resolved before recovery window: %s", state)
	}

	resolvedAt := recoveryStart.Add(observability.AlertRecoveryDelay)
	if err := engine.EvaluateOnce(ctx, resolvedAt); err != nil {
		t.Fatalf("resolve alert: %v", err)
	}
	var resolvedState string
	if err := pool.QueryRow(ctx, `SELECT condition_state FROM observability_alerts WHERE id=$1::uuid`, firstID).Scan(&resolvedState); err != nil {
		t.Fatalf("read resolved alert: %v", err)
	}
	if resolvedState != "resolved" {
		t.Fatalf("resolved episode state = %s", resolvedState)
	}

	recurrenceAt := resolvedAt.Add(10 * time.Second)
	applyAlertTestHealth(t, ctx, healthRepository, resource, observability.HealthUnhealthy, "disk_free_critical", recurrenceAt)
	if err := engine.EvaluateOnce(ctx, recurrenceAt); err != nil {
		t.Fatalf("create recurrent pending alert: %v", err)
	}
	secondID, state, _, _ := readActiveAlert(t, ctx, pool, resource.ID)
	if secondID == firstID || state != "pending" {
		t.Fatalf("recurrence did not create a new pending episode: first=%s second=%s state=%s", firstID, secondID, state)
	}
	if err := engine.EvaluateOnce(ctx, recurrenceAt.Add(time.Minute)); err != nil {
		t.Fatalf("evaluate recurrent flap delay: %v", err)
	}
	_, state, _, _ = readActiveAlert(t, ctx, pool, resource.ID)
	if state != "pending" {
		t.Fatalf("fast recurrence bypassed flap protection: %s", state)
	}
	if err := engine.EvaluateOnce(ctx, recurrenceAt.Add(observability.AlertFlapFireDelay)); err != nil {
		t.Fatalf("fire recurrent alert after flap delay: %v", err)
	}
	_, state, _, _ = readActiveAlert(t, ctx, pool, resource.ID)
	if state != "firing" {
		t.Fatalf("recurrent alert state = %s, want firing", state)
	}
}

func TestAlertEngineClearsTransientPendingWithoutInheritedTime(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := Connect(ctx, databaseURL, logger)
	if err != nil {
		t.Fatalf("connect to test PostgreSQL: %v", err)
	}
	defer pool.Close()

	resetPublicSchema(t, ctx, pool)
	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	healthRepository := observability.NewHealthRepository(pool)
	engine := observability.NewAlertEngine(logger, pool)
	base := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)
	resource := observability.ResourceRef{Type: "server", ID: "alert-server-2"}

	applyAlertTestHealth(t, ctx, healthRepository, resource, observability.HealthUnhealthy, "disk_free_critical", base)
	if err := engine.EvaluateOnce(ctx, base); err != nil {
		t.Fatalf("create transient pending: %v", err)
	}
	firstID, state, _, _ := readActiveAlert(t, ctx, pool, resource.ID)
	if state != "pending" {
		t.Fatalf("initial transient state = %s", state)
	}

	clearAt := base.Add(10 * time.Second)
	applyAlertTestHealth(t, ctx, healthRepository, resource, observability.HealthHealthy, "disk_capacity_ok", clearAt)
	if err := engine.EvaluateOnce(ctx, clearAt); err != nil {
		t.Fatalf("clear transient pending: %v", err)
	}
	var firstState string
	if err := pool.QueryRow(ctx, `SELECT condition_state FROM observability_alerts WHERE id=$1::uuid`, firstID).Scan(&firstState); err != nil {
		t.Fatalf("read transient episode: %v", err)
	}
	if firstState != "resolved" {
		t.Fatalf("transient pending must resolve immediately, got %s", firstState)
	}

	recurAt := clearAt.Add(10 * time.Second)
	applyAlertTestHealth(t, ctx, healthRepository, resource, observability.HealthUnhealthy, "disk_free_critical", recurAt)
	if err := engine.EvaluateOnce(ctx, recurAt); err != nil {
		t.Fatalf("create fresh recurrence: %v", err)
	}
	secondID, state, _, _ := readActiveAlert(t, ctx, pool, resource.ID)
	if secondID == firstID || state != "pending" {
		t.Fatalf("fresh recurrence = id=%s state=%s; first=%s", secondID, state, firstID)
	}
}

func applyAlertTestHealth(t *testing.T, ctx context.Context, repository *observability.HealthRepository, resource observability.ResourceRef, state observability.HealthState, reason string, now time.Time) {
	t.Helper()
	expiresAt := now.Add(10 * time.Minute)
	summary := "Root filesystem capacity is healthy."
	if state == observability.HealthUnhealthy {
		summary = "Root filesystem free space is critically low."
	}
	check := observability.HealthCheck{
		Key:        observability.CheckHostDiskCapacity,
		Resource:   resource,
		Component:  "host",
		State:      state,
		Required:   true,
		ReasonCode: reason,
		Summary:    summary,
		ObservedAt: now,
		ExpiresAt:  &expiresAt,
	}
	if err := repository.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("apply health check: %v", err)
	}
}

func readActiveAlert(t *testing.T, ctx context.Context, pool *pgxpool.Pool, resourceID string) (string, string, string, *time.Time) {
	t.Helper()
	var id string
	var state string
	var severity string
	var recoveryStarted *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT id::text, condition_state, severity, recovery_started_at
		FROM observability_alerts
		WHERE resource_id=$1 AND condition_state IN ('pending','firing')
		ORDER BY started_at DESC
		LIMIT 1
	`, resourceID).Scan(&id, &state, &severity, &recoveryStarted); err != nil {
		t.Fatalf("read active alert: %v", err)
	}
	return id, state, severity, recoveryStarted
}
