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

func TestHealthRepositoryPersistsCurrentStateAndTransitions(t *testing.T) {
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

	repository := observability.NewHealthRepository(pool)
	now := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	expiresAt := now.Add(90 * time.Second)
	check := observability.HealthCheck{
		Key:        observability.CheckHostDiskCapacity,
		Resource:   observability.ResourceRef{Type: "server", ID: "server-1"},
		Component:  "host",
		State:      observability.HealthHealthy,
		Required:   true,
		ReasonCode: "disk_capacity_ok",
		Summary:    "Root filesystem capacity is healthy.",
		Evidence:   []byte(`{"freePct":40}`),
		ObservedAt: now,
		ExpiresAt:  &expiresAt,
	}
	if err := repository.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("persist first health check: %v", err)
	}

	check.ObservedAt = now.Add(30 * time.Second)
	expiresAt = check.ObservedAt.Add(90 * time.Second)
	check.ExpiresAt = &expiresAt
	if err := repository.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("refresh same health state: %v", err)
	}

	var transitionCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM observability_health_transitions
		WHERE resource_type='server' AND resource_id='server-1' AND check_key=$1
	`, observability.CheckHostDiskCapacity).Scan(&transitionCount); err != nil {
		t.Fatalf("count health transitions: %v", err)
	}
	if transitionCount != 1 {
		t.Fatalf("same-state refresh created %d transitions, want 1 initial transition", transitionCount)
	}

	check.State = observability.HealthUnhealthy
	check.ReasonCode = "disk_free_critical"
	check.Summary = "Root filesystem free space is critically low."
	check.ObservedAt = now.Add(60 * time.Second)
	expiresAt = check.ObservedAt.Add(90 * time.Second)
	check.ExpiresAt = &expiresAt
	if err := repository.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("persist health state transition: %v", err)
	}

	var currentState string
	var previousState string
	var transitionState string
	if err := pool.QueryRow(ctx, `
		SELECT state
		FROM observability_current_health
		WHERE resource_type='server' AND resource_id='server-1' AND check_key=$1
	`, observability.CheckHostDiskCapacity).Scan(&currentState); err != nil {
		t.Fatalf("read current health state: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT previous_state, state
		FROM observability_health_transitions
		WHERE resource_type='server' AND resource_id='server-1' AND check_key=$1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, observability.CheckHostDiskCapacity).Scan(&previousState, &transitionState); err != nil {
		t.Fatalf("read latest health transition: %v", err)
	}
	if currentState != "unhealthy" || previousState != "healthy" || transitionState != "unhealthy" {
		t.Fatalf("unexpected health transition: current=%s previous=%s next=%s", currentState, previousState, transitionState)
	}
}
