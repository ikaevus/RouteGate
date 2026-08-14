CREATE TABLE observability_agent_telemetry (
    agent_id UUID PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    schema_version INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    host_load_1 DOUBLE PRECISION,
    host_load_5 DOUBLE PRECISION,
    host_load_15 DOUBLE PRECISION,
    host_logical_cpus INTEGER,
    host_memory_total_bytes BIGINT,
    host_memory_available_bytes BIGINT,
    host_root_fs_total_bytes BIGINT,
    host_root_fs_free_bytes BIGINT,
    host_uptime_seconds BIGINT,
    vpn_core_type TEXT NOT NULL,
    vpn_core_installed BOOLEAN NOT NULL,
    vpn_core_version TEXT,
    vpn_core_service_state TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT observability_agent_telemetry_schema_version_check CHECK (schema_version >= 1),
    CONSTRAINT observability_agent_telemetry_host_load_1_check CHECK (host_load_1 IS NULL OR host_load_1 >= 0),
    CONSTRAINT observability_agent_telemetry_host_load_5_check CHECK (host_load_5 IS NULL OR host_load_5 >= 0),
    CONSTRAINT observability_agent_telemetry_host_load_15_check CHECK (host_load_15 IS NULL OR host_load_15 >= 0),
    CONSTRAINT observability_agent_telemetry_host_logical_cpus_check CHECK (host_logical_cpus IS NULL OR host_logical_cpus > 0),
    CONSTRAINT observability_agent_telemetry_host_memory_total_bytes_check CHECK (host_memory_total_bytes IS NULL OR host_memory_total_bytes > 0),
    CONSTRAINT observability_agent_telemetry_host_memory_available_bytes_check CHECK (
        host_memory_available_bytes IS NULL OR host_memory_available_bytes >= 0
    ),
    CONSTRAINT observability_agent_telemetry_host_memory_bounds_check CHECK (
        host_memory_total_bytes IS NULL
        OR host_memory_available_bytes IS NULL
        OR host_memory_available_bytes <= host_memory_total_bytes
    ),
    CONSTRAINT observability_agent_telemetry_host_root_fs_total_bytes_check CHECK (host_root_fs_total_bytes IS NULL OR host_root_fs_total_bytes > 0),
    CONSTRAINT observability_agent_telemetry_host_root_fs_free_bytes_check CHECK (
        host_root_fs_free_bytes IS NULL OR host_root_fs_free_bytes >= 0
    ),
    CONSTRAINT observability_agent_telemetry_host_root_fs_bounds_check CHECK (
        host_root_fs_total_bytes IS NULL
        OR host_root_fs_free_bytes IS NULL
        OR host_root_fs_free_bytes <= host_root_fs_total_bytes
    ),
    CONSTRAINT observability_agent_telemetry_host_uptime_seconds_check CHECK (host_uptime_seconds IS NULL OR host_uptime_seconds >= 0),
    CONSTRAINT observability_agent_telemetry_vpn_core_type_not_blank CHECK (btrim(vpn_core_type) <> ''),
    CONSTRAINT observability_agent_telemetry_vpn_core_version_not_blank CHECK (
        vpn_core_version IS NULL OR btrim(vpn_core_version) <> ''
    ),
    CONSTRAINT observability_agent_telemetry_vpn_core_service_state_not_blank CHECK (btrim(vpn_core_service_state) <> '')
);

CREATE UNIQUE INDEX idx_observability_agent_telemetry_server
    ON observability_agent_telemetry (server_id);

CREATE INDEX idx_observability_agent_telemetry_collected_at
    ON observability_agent_telemetry (collected_at DESC);
