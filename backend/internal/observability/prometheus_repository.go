package observability

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MetricCount struct {
	Labels map[string]string
	Value  int64
}

type ManagerMetricsSnapshot struct {
	PostgreSQLUp         bool
	CollectionSuccessful bool
	AppliedSchemaVersion int
	Agents               []MetricCount
	Alerts               []MetricCount
	Diagnostics          []MetricCount
	Deliveries           []MetricCount
}

type FleetNodeMetrics struct {
	ServerID             string
	AgentStatus          string
	ReceivedAt           time.Time
	Load1                *float64
	Load5                *float64
	Load15               *float64
	LogicalCPUs          *int64
	MemoryTotalBytes     *int64
	MemoryAvailableBytes *int64
	RootFSTotalBytes     *int64
	RootFSFreeBytes      *int64
	UptimeSeconds        *int64
	VPNCoreType          string
	VPNCoreInstalled     bool
	VPNCoreVersion       string
	VPNCoreServiceState  string
}

type PrometheusRepository interface {
	ManagerSnapshot(context.Context) ManagerMetricsSnapshot
	FleetSnapshot(context.Context) ([]FleetNodeMetrics, error)
	CurrentHealth(context.Context) ([]HealthCheck, error)
}

type PostgreSQLPrometheusRepository struct {
	pool *pgxpool.Pool
}

func NewPrometheusRepository(pool *pgxpool.Pool) *PostgreSQLPrometheusRepository {
	return &PostgreSQLPrometheusRepository{pool: pool}
}

func (r *PostgreSQLPrometheusRepository) ManagerSnapshot(ctx context.Context) ManagerMetricsSnapshot {
	snapshot := ManagerMetricsSnapshot{CollectionSuccessful: true}
	if err := r.pool.Ping(ctx); err != nil {
		snapshot.CollectionSuccessful = false
		return snapshot
	}
	snapshot.PostgreSQLUp = true

	var version string
	if err := r.pool.QueryRow(ctx, `
		SELECT version
		FROM schema_migrations
		ORDER BY applied_at DESC, version DESC
		LIMIT 1
	`).Scan(&version); err != nil {
		snapshot.CollectionSuccessful = false
	} else {
		snapshot.AppliedSchemaVersion = numericMigrationVersion(version)
	}

	var ok bool
	snapshot.Agents, ok = r.groupCounts(ctx, `SELECT status, COUNT(*) FROM agents GROUP BY status ORDER BY status`, []string{"status"})
	snapshot.CollectionSuccessful = snapshot.CollectionSuccessful && ok
	snapshot.Alerts, ok = r.groupCounts(ctx, `
		SELECT severity, condition_state, COUNT(*)
		FROM observability_alerts
		WHERE condition_state IN ('pending','firing')
		GROUP BY severity, condition_state
		ORDER BY severity, condition_state
	`, []string{"severity", "state"})
	snapshot.CollectionSuccessful = snapshot.CollectionSuccessful && ok
	snapshot.Diagnostics, ok = r.groupCounts(ctx, `
		SELECT profile_key, status, COUNT(*)
		FROM observability_diagnostic_runs
		GROUP BY profile_key, status
		ORDER BY profile_key, status
	`, []string{"profile", "status"})
	snapshot.CollectionSuccessful = snapshot.CollectionSuccessful && ok
	snapshot.Deliveries, ok = r.groupCounts(ctx, `SELECT status, COUNT(*) FROM deliveries GROUP BY status ORDER BY status`, []string{"status"})
	snapshot.CollectionSuccessful = snapshot.CollectionSuccessful && ok
	return snapshot
}

func (r *PostgreSQLPrometheusRepository) groupCounts(ctx context.Context, query string, labelNames []string) ([]MetricCount, bool) {
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, false
	}
	defer rows.Close()

	items := make([]MetricCount, 0)
	for rows.Next() {
		values := make([]string, len(labelNames))
		dest := make([]any, 0, len(labelNames)+1)
		for i := range values {
			dest = append(dest, &values[i])
		}
		var count int64
		dest = append(dest, &count)
		if err := rows.Scan(dest...); err != nil {
			return nil, false
		}
		labels := make(map[string]string, len(labelNames))
		for i, name := range labelNames {
			labels[name] = values[i]
		}
		items = append(items, MetricCount{Labels: labels, Value: count})
	}
	if err := rows.Err(); err != nil {
		return nil, false
	}
	return items, true
}

func (r *PostgreSQLPrometheusRepository) FleetSnapshot(ctx context.Context) ([]FleetNodeMetrics, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			t.server_id::text,
			a.status,
			t.received_at,
			t.host_load_1,
			t.host_load_5,
			t.host_load_15,
			t.host_logical_cpus,
			t.host_memory_total_bytes,
			t.host_memory_available_bytes,
			t.host_root_fs_total_bytes,
			t.host_root_fs_free_bytes,
			t.host_uptime_seconds,
			t.vpn_core_type,
			t.vpn_core_installed,
			t.vpn_core_version,
			t.vpn_core_service_state
		FROM observability_agent_telemetry t
		JOIN agents a ON a.id=t.agent_id
		ORDER BY t.server_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]FleetNodeMetrics, 0)
	for rows.Next() {
		var item FleetNodeMetrics
		var load1, load5, load15 pgtype.Float8
		var logicalCPUs pgtype.Int4
		var memoryTotal, memoryAvailable, rootTotal, rootFree, uptime pgtype.Int8
		var coreVersion pgtype.Text
		if err := rows.Scan(
			&item.ServerID,
			&item.AgentStatus,
			&item.ReceivedAt,
			&load1,
			&load5,
			&load15,
			&logicalCPUs,
			&memoryTotal,
			&memoryAvailable,
			&rootTotal,
			&rootFree,
			&uptime,
			&item.VPNCoreType,
			&item.VPNCoreInstalled,
			&coreVersion,
			&item.VPNCoreServiceState,
		); err != nil {
			return nil, err
		}
		item.Load1 = float8Pointer(load1)
		item.Load5 = float8Pointer(load5)
		item.Load15 = float8Pointer(load15)
		item.LogicalCPUs = int4Pointer(logicalCPUs)
		item.MemoryTotalBytes = int8Pointer(memoryTotal)
		item.MemoryAvailableBytes = int8Pointer(memoryAvailable)
		item.RootFSTotalBytes = int8Pointer(rootTotal)
		item.RootFSFreeBytes = int8Pointer(rootFree)
		item.UptimeSeconds = int8Pointer(uptime)
		if coreVersion.Valid {
			item.VPNCoreVersion = coreVersion.String
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgreSQLPrometheusRepository) CurrentHealth(ctx context.Context) ([]HealthCheck, error) {
	return NewAlertRepository(r.pool).ListCurrentHealth(ctx)
}

func numericMigrationVersion(version string) int {
	prefix, _, _ := strings.Cut(strings.TrimSpace(version), "_")
	value, err := strconv.Atoi(prefix)
	if err != nil {
		return 0
	}
	return value
}

func float8Pointer(value pgtype.Float8) *float64 {
	if !value.Valid {
		return nil
	}
	v := value.Float64
	return &v
}

func int4Pointer(value pgtype.Int4) *int64 {
	if !value.Valid {
		return nil
	}
	v := int64(value.Int32)
	return &v
}

func int8Pointer(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}
