-- Repair the vpn_core_install operation constraint on historical hosts.
--
-- Migration 000104 originally allowed only install_sing_box. RG-114 later
-- generalized the existing Agent installation channel to WireGuard, Hysteria2
-- and MTProto, but long-lived databases still retain the old CHECK constraint.
-- A fresh repair migration is required because 000104 is already recorded as
-- applied on those hosts.
--
-- The 000131zz prefix is deliberate: this remains a new migration while the
-- canonical reported schema version stays 000132_safe_client_protocol_activation.

ALTER TABLE agent_operation_jobs
    DROP CONSTRAINT IF EXISTS agent_operation_jobs_operation_check;

ALTER TABLE agent_operation_jobs
    ADD CONSTRAINT agent_operation_jobs_operation_check
    CHECK (
        (kind = 'vpn_core_service' AND operation IN ('start', 'stop', 'restart'))
        OR
        (kind = 'vpn_core_install' AND operation IN (
            'install_sing_box',
            'install_wireguard',
            'install_hysteria2',
            'install_mtg'
        ))
        OR
        (kind = 'diagnostic' AND operation IN (
            'host_overview',
            'vpn_core_status',
            'manager_certificate'
        ))
    );
