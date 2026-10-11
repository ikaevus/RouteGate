-- Never erase recovery history or release a reservation through downgrade.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM runtime_recovery_preparations) THEN
 RAISE EXCEPTION 'runtime_recovery_history_retained'; END IF;
END; $$;
DROP TRIGGER rg142_revocation_preparation ON vpn_credential_revocations;
CREATE OR REPLACE FUNCTION routegate_revocation_node_guard(node uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 IF node IS NULL THEN RETURN; END IF;
 PERFORM id FROM servers WHERE id=node FOR UPDATE;
 IF EXISTS(SELECT 1 FROM vpn_credential_revocations WHERE server_id=node AND state='prepared') THEN
 RAISE EXCEPTION USING ERRCODE='P0141',MESSAGE='node_reserved_for_revocation'; END IF;
END; $$;
CREATE OR REPLACE FUNCTION routegate_revocation_routing_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM vpn_credential_revocations WHERE state='prepared') THEN RAISE EXCEPTION USING ERRCODE='P0141',MESSAGE='routing_reserved_for_revocation'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END; $$;
CREATE OR REPLACE FUNCTION routegate_revocation_manager_update_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.operation='apply' THEN
 PERFORM pg_advisory_xact_lock(140159);
 IF EXISTS(SELECT 1 FROM vpn_credential_revocations WHERE state='prepared') THEN RAISE EXCEPTION USING ERRCODE='P0141',MESSAGE='manager_update_reserved_for_revocation'; END IF;
 END IF; RETURN NEW;
END; $$;
DROP TABLE runtime_recovery_events;
DROP TABLE runtime_recovery_preparations;
DROP FUNCTION routegate_runtime_recovery_audit();
DROP FUNCTION routegate_runtime_recovery_ledger_guard();
