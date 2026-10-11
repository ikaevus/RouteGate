-- Internal, never-dispatched preparations. Cancellation releases ONLY Manager
-- admission; it cannot clear the Agent's durable runtime fence.
CREATE TABLE runtime_recovery_preparations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 server_id uuid NOT NULL REFERENCES servers(id),
 agent_id uuid NOT NULL REFERENCES agents(id),
 agent_credential_generation bigint NOT NULL,
 task_id uuid NOT NULL,
 marker_hash text NOT NULL CHECK(marker_hash ~ '^[0-9a-f]{64}$'),
 receipt_hash text NOT NULL CHECK(receipt_hash ~ '^[0-9a-f]{64}$'),
 envelope_hash text NOT NULL CHECK(envelope_hash ~ '^[0-9a-f]{64}$'),
 state text NOT NULL DEFAULT 'prepared' CHECK(state IN ('prepared','cancelled')),
 requested_by text NOT NULL CHECK(length(trim(requested_by)) BETWEEN 1 AND 200),
 last_action_by text NOT NULL CHECK(length(trim(last_action_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 cancelled_at timestamptz,
 CHECK((state='cancelled')=(cancelled_at IS NOT NULL))
);
CREATE UNIQUE INDEX runtime_recovery_one_node ON runtime_recovery_preparations(server_id) WHERE state='prepared';
CREATE TABLE runtime_recovery_events (
 sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 operation_id uuid NOT NULL REFERENCES runtime_recovery_preparations(id),
 action text NOT NULL CHECK(action IN ('prepared','cancelled')),
 actor text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);

-- Reuse all existing admission call sites, without weakening revocation guards.
CREATE OR REPLACE FUNCTION routegate_revocation_node_guard(node uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 IF node IS NULL THEN RETURN; END IF;
 PERFORM id FROM servers WHERE id=node FOR UPDATE;
 IF EXISTS(SELECT 1 FROM vpn_credential_revocations WHERE server_id=node AND state='prepared') THEN
 RAISE EXCEPTION USING ERRCODE='P0141',MESSAGE='node_reserved_for_revocation'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_recovery_preparations WHERE server_id=node AND state='prepared') THEN
 RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='node_reserved_for_runtime_recovery'; END IF;
END; $$;
CREATE OR REPLACE FUNCTION routegate_revocation_routing_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM vpn_credential_revocations WHERE state='prepared') THEN RAISE EXCEPTION USING ERRCODE='P0141',MESSAGE='routing_reserved_for_revocation'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_recovery_preparations WHERE state='prepared') THEN RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='routing_reserved_for_runtime_recovery'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END; $$;
CREATE OR REPLACE FUNCTION routegate_revocation_manager_update_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.operation='apply' THEN
 PERFORM pg_advisory_xact_lock(140159);
 IF EXISTS(SELECT 1 FROM vpn_credential_revocations WHERE state='prepared') THEN RAISE EXCEPTION USING ERRCODE='P0141',MESSAGE='manager_update_reserved_for_revocation'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_recovery_preparations WHERE state='prepared') THEN RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='manager_update_reserved_for_runtime_recovery'; END IF;
 END IF; RETURN NEW;
END; $$;

CREATE FUNCTION routegate_runtime_recovery_ledger_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE eligible integer;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='runtime_recovery_history_retained'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (to_jsonb(OLD)-ARRAY['state','last_action_by','cancelled_at']) IS DISTINCT FROM
     (to_jsonb(NEW)-ARRAY['state','last_action_by','cancelled_at']) OR OLD.state<>'prepared' OR NEW.state<>'cancelled' THEN
   RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='runtime_recovery_invalid_transition';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.state<>'prepared' OR NEW.requested_by<>NEW.last_action_by THEN RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='runtime_recovery_invalid_initial_state'; END IF;
 PERFORM pg_advisory_xact_lock(140159);
 LOCK TABLE routing_profiles,routing_profile_rules,server_routing_profiles,managed_routing_rule_sets IN SHARE MODE;
 PERFORM routegate_transfer_node_guard(NEW.server_id);
 PERFORM routegate_revocation_node_guard(NEW.server_id);
 IF NOT EXISTS(SELECT 1 FROM agents a JOIN servers s ON s.id=a.server_id
 WHERE a.id=NEW.agent_id AND a.server_id=NEW.server_id AND a.credential_generation=NEW.agent_credential_generation
 AND s.status='active' AND s.deployment_role IN ('vpn','hybrid') AND s.vpn_protocol='vless'
 AND a.status='online' AND a.last_authenticated_heartbeat_at>now()-interval '90 seconds'
 AND a.last_authenticated_heartbeat_generation=a.credential_generation
 AND a.capabilities @> '{"runtimeMutationFencingV1":true}'::jsonb) THEN
 RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='runtime_recovery_agent_not_ready'; END IF;
 SELECT count(*) INTO eligible FROM (
 SELECT id FROM config_apply_jobs WHERE id=NEW.task_id AND server_id=NEW.server_id AND agent_id=NEW.agent_id AND action='apply' AND status='succeeded' AND COALESCE(error_message,'')=''
 UNION ALL SELECT id FROM agent_operation_jobs WHERE id=NEW.task_id AND server_id=NEW.server_id AND agent_id=NEW.agent_id AND kind='vpn_core_service' AND operation='restart' AND status='succeeded' AND COALESCE(error_message,'')=''
 ) jobs;
 IF eligible<>1 OR EXISTS(SELECT 1 FROM config_apply_jobs WHERE server_id=NEW.server_id AND status IN ('pending','in_progress'))
 OR EXISTS(SELECT 1 FROM agent_operation_jobs WHERE server_id=NEW.server_id AND status IN ('pending','in_progress'))
 OR EXISTS(SELECT 1 FROM agent_platform_update_jobs WHERE server_id=NEW.server_id AND status IN ('pending','in_progress','mutation_dispatched','outcome_unknown'))
 OR EXISTS(SELECT 1 FROM update_jobs WHERE operation='apply' AND status IN ('pending','running')) THEN
 RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='runtime_recovery_conflict'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER rg142_ledger BEFORE INSERT OR UPDATE OR DELETE ON runtime_recovery_preparations FOR EACH ROW EXECUTE FUNCTION routegate_runtime_recovery_ledger_guard();
CREATE FUNCTION routegate_runtime_recovery_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO runtime_recovery_events(operation_id,action,actor) VALUES(NEW.id,NEW.state,NEW.last_action_by);
 RETURN NEW;
END; $$;
CREATE TRIGGER rg142_audit AFTER INSERT OR UPDATE ON runtime_recovery_preparations FOR EACH ROW EXECUTE FUNCTION routegate_runtime_recovery_audit();
CREATE TRIGGER rg142_audit_immutable BEFORE UPDATE OR DELETE ON runtime_recovery_events FOR EACH ROW EXECUTE FUNCTION routegate_revocation_audit_guard();
-- Revocation preparation itself does not enqueue a job: fence its insertion too.
CREATE TRIGGER rg142_revocation_preparation BEFORE INSERT ON vpn_credential_revocations FOR EACH ROW EXECUTE FUNCTION routegate_revocation_job_guard();
