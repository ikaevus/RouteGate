package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/analytics"
)

func TestAnalyticsOverviewProjectsLocationHealthTelemetryAndAlerts(t *testing.T) {
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

	var serverID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (
			name,status,provider,public_ip,location,
			location_country,location_city,location_latitude,location_longitude,location_source
		) VALUES (
			'Frankfurt Edge','active','ExampleCloud','203.0.113.25'::inet,'Germany',
			'Germany','Frankfurt',50.1109,8.6821,'manual'
		) RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}

	var agentID string
	now := time.Now().UTC()
	if err := pool.QueryRow(ctx, `
		INSERT INTO agents (
			server_id,hostname,os,arch,agent_version,protocol_version,status,token_hash,capabilities,registered_at,last_seen_at
		) VALUES (
			$1::uuid,'de-edge','linux','amd64','test',1,'online','analytics-test-token','{}'::jsonb,$2,$2
		) RETURNING id::text
	`, serverID, now).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO observability_agent_telemetry (
			agent_id,server_id,schema_version,collected_at,received_at,
			host_load_1,host_logical_cpus,
			host_memory_total_bytes,host_memory_available_bytes,
			host_root_fs_total_bytes,host_root_fs_free_bytes,host_uptime_seconds,
			vpn_core_type,vpn_core_installed,vpn_core_version,vpn_core_service_state
		) VALUES (
			$1::uuid,$2::uuid,1,$3,$3,
			0.75,4,1000,500,2000,200,7200,
			'sing-box',true,'1.12.0','active'
		)
	`, agentID, serverID, now); err != nil {
		t.Fatalf("create telemetry: %v", err)
	}

	expiresAt := now.Add(90 * time.Second)
	if _, err := pool.Exec(ctx, `
		INSERT INTO observability_current_health (
			resource_type,resource_id,check_key,component,state,required,reason_code,summary,recommended_action,evidence,observed_at,expires_at
		) VALUES
		('server',$1,'agent.telemetry.freshness','agent','healthy',true,'telemetry_recent','Agent telemetry is current.','', '{}'::jsonb,$2,$3),
		('server',$1,'host.disk.capacity','host','degraded',true,'disk_free_low','Root filesystem free space is low.','free_disk_space','{}'::jsonb,$2,$3)
	`, serverID, now, expiresAt); err != nil {
		t.Fatalf("create health: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO observability_alerts (
			fingerprint,rule_key,resource_type,resource_id,severity,condition_state,summary,reason_code,started_at,firing_at,last_evaluated_at
		) VALUES (
			'host.disk.capacity:server:' || $1,'host.disk.capacity','server',$1,
			'critical','firing','Disk capacity requires attention.','disk_free_critical',$2,$2,$2
		)
	`, serverID, now); err != nil {
		t.Fatalf("create alert: %v", err)
	}

	overview, err := analytics.NewRepository(pool).Overview(ctx)
	if err != nil {
		t.Fatalf("load analytics overview: %v", err)
	}
	if overview.Summary.TotalNodes != 1 || overview.Summary.DegradedNodes != 1 || overview.Summary.LocatedNodes != 1 {
		t.Fatalf("unexpected summary: %+v", overview.Summary)
	}
	if overview.Summary.ActiveAlerts != 1 || overview.Summary.CriticalAlerts != 1 {
		t.Fatalf("unexpected alert summary: %+v", overview.Summary)
	}
	if len(overview.Nodes) != 1 {
		t.Fatalf("nodes=%d, want 1", len(overview.Nodes))
	}
	node := overview.Nodes[0]
	if node.Location.Latitude == nil || node.Location.Longitude == nil || node.Location.City != "Frankfurt" {
		t.Fatalf("unexpected location: %+v", node.Location)
	}
	if node.Health.State != "degraded" || node.Health.RecommendedAction != "free_disk_space" {
		t.Fatalf("unexpected health: %+v", node.Health)
	}
	if node.Resources.MemoryUsageRatio == nil || *node.Resources.MemoryUsageRatio != 0.5 {
		t.Fatalf("unexpected memory ratio: %+v", node.Resources.MemoryUsageRatio)
	}
	if node.Resources.RootFSUsageRatio == nil || *node.Resources.RootFSUsageRatio != 0.9 {
		t.Fatalf("unexpected disk ratio: %+v", node.Resources.RootFSUsageRatio)
	}
	if !node.Agent.ObservationFresh || node.AlertCount != 1 || !node.HasCritical {
		t.Fatalf("unexpected operational state: %+v", node)
	}
}

func TestServerGeographyRejectsPartialCoordinates(t *testing.T) {
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
		INSERT INTO servers (name,status,location_latitude,location_source)
		VALUES ('Invalid Map Node','pending',50.0,'manual')
	`); err == nil {
		t.Fatal("database must reject latitude without longitude")
	}
}
