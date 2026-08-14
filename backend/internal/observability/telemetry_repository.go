package observability

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TelemetryRepository struct {
	pool *pgxpool.Pool
}

func NewTelemetryRepository(pool *pgxpool.Pool) *TelemetryRepository {
	return &TelemetryRepository{pool: pool}
}

// UpsertAgentTelemetry persists exactly one latest snapshot per Agent. It does
// not create metric history; historical time-series retention remains outside
// PostgreSQL in Prometheus-compatible infrastructure when enabled.
func (r *TelemetryRepository) UpsertAgentTelemetry(
	ctx context.Context,
	agentID string,
	serverID string,
	snapshot AgentTelemetrySnapshot,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO observability_agent_telemetry (
			agent_id,
			server_id,
			schema_version,
			collected_at,
			received_at,
			host_load_1,
			host_load_5,
			host_load_15,
			host_logical_cpus,
			host_memory_total_bytes,
			host_memory_available_bytes,
			host_root_fs_total_bytes,
			host_root_fs_free_bytes,
			host_uptime_seconds,
			vpn_core_type,
			vpn_core_installed,
			vpn_core_version,
			vpn_core_service_state,
			updated_at
		)
		VALUES (
			$1::uuid,
			$2::uuid,
			$3,
			$4,
			now(),
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			$11,
			$12,
			$13,
			$14,
			$15,
			NULLIF($16, ''),
			$17,
			now()
		)
		ON CONFLICT (agent_id)
		DO UPDATE SET
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
	`,
		agentID,
		serverID,
		snapshot.SchemaVersion,
		snapshot.CollectedAt,
		snapshot.Host.Load1,
		snapshot.Host.Load5,
		snapshot.Host.Load15,
		snapshot.Host.LogicalCPUs,
		snapshot.Host.MemoryTotalBytes,
		snapshot.Host.MemoryAvailableBytes,
		snapshot.Host.RootFSTotalBytes,
		snapshot.Host.RootFSFreeBytes,
		snapshot.Host.UptimeSeconds,
		snapshot.VPNCore.Type,
		snapshot.VPNCore.Installed,
		snapshot.VPNCore.Version,
		snapshot.VPNCore.ServiceState,
	)
	return err
}
