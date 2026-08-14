package agents

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type telemetryStore struct {
	pool *pgxpool.Pool
}

func newTelemetryStore(pool *pgxpool.Pool) *telemetryStore {
	return &telemetryStore{pool: pool}
}

func (s *telemetryStore) UpsertAgentTelemetry(ctx context.Context, agentID, serverID string, t HeartbeatTelemetry) error {
	memoryTotal, err := telemetryBigint(t.Host.MemoryTotalBytes)
	if err != nil {
		return err
	}
	memoryAvailable, err := telemetryBigint(t.Host.MemoryAvailableBytes)
	if err != nil {
		return err
	}
	rootTotal, err := telemetryBigint(t.Host.RootFSTotalBytes)
	if err != nil {
		return err
	}
	rootFree, err := telemetryBigint(t.Host.RootFSFreeBytes)
	if err != nil {
		return err
	}
	uptime, err := telemetryBigint(t.Host.UptimeSeconds)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO observability_agent_telemetry (
			agent_id, server_id, schema_version, collected_at,
			host_load_1, host_load_5, host_load_15, host_logical_cpus,
			host_memory_total_bytes, host_memory_available_bytes,
			host_root_fs_total_bytes, host_root_fs_free_bytes, host_uptime_seconds,
			vpn_core_type, vpn_core_installed, vpn_core_version, vpn_core_service_state
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NULLIF($16, ''), $17)
		ON CONFLICT (agent_id) DO UPDATE SET
			server_id = EXCLUDED.server_id,
			schema_version = EXCLUDED.schema_version,
			collected_at = EXCLUDED.collected_at,
			received_at = now(),
			host_load_1 = EXCLUDED.host_load_1,
			host_load_5 = EXCLUDED.host_load_5,
			host_load_15 = EXCLUDED.host_load_15,
			host_logical_cpus = EXCLUDED.host_logical_cpus,
			host_memory_total_bytes = EXCLUDED.host_memory_total_bytes,
			host_memory_available_bytes = EXCLUDED.host_memory_available_bytes,
			host_root_fs_total_bytes = EXCLUDED.host_root_fs_total_bytes,
			host_root_fs_free_bytes = EXCLUDED.host_root_fs_free_bytes,
			host_uptime_seconds = EXCLUDED.host_uptime_seconds,
			vpn_core_type = EXCLUDED.vpn_core_type,
			vpn_core_installed = EXCLUDED.vpn_core_installed,
			vpn_core_version = EXCLUDED.vpn_core_version,
			vpn_core_service_state = EXCLUDED.vpn_core_service_state,
			updated_at = now()
	`, agentID, serverID, t.SchemaVersion, t.CollectedAt,
		t.Host.Load1, t.Host.Load5, t.Host.Load15, t.Host.LogicalCPUs,
		memoryTotal, memoryAvailable, rootTotal, rootFree, uptime,
		strings.TrimSpace(t.VPNCore.Type), t.VPNCore.Installed, strings.TrimSpace(t.VPNCore.Version), strings.TrimSpace(t.VPNCore.ServiceState))
	return err
}

func telemetryBigint(value *uint64) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	if *value > math.MaxInt64 {
		return nil, fmt.Errorf("telemetry value exceeds PostgreSQL BIGINT")
	}
	converted := int64(*value)
	return &converted, nil
}
