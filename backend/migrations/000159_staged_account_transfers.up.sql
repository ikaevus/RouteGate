-- RG-140: durable, direction-independent transfer. No plaintext subscription
-- material is stored. Completion requires verified deployment of the cleanup.
CREATE TABLE vpn_account_transfers (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 vpn_account_id uuid NOT NULL REFERENCES vpn_accounts(id) ON DELETE CASCADE,
 source_server_id uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
 target_server_id uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
 source_version_id uuid NOT NULL REFERENCES config_versions(id) ON DELETE CASCADE,
 target_version_id uuid REFERENCES config_versions(id) ON DELETE CASCADE,
 target_job_id uuid REFERENCES config_apply_jobs(id) ON DELETE SET NULL,
 cleanup_version_id uuid REFERENCES config_versions(id) ON DELETE CASCADE,
 cleanup_job_id uuid REFERENCES config_apply_jobs(id) ON DELETE SET NULL,
 state text NOT NULL CHECK (state IN ('preparing','target_applying','target_ready','client_refresh_pending','source_cleaning','target_cleaning','complete','cancelled','rolled_back')),
 requested_by text NOT NULL,
 last_action_by text NOT NULL,
 last_error text NOT NULL DEFAULT '',
 device_inventory jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(device_inventory)='array'),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 cutover_at timestamptz,
 cleanup_acknowledged_at timestamptz,
 completed_at timestamptz,
 CHECK (source_server_id <> target_server_id),
 CHECK ((completed_at IS NOT NULL) = (state IN ('complete','cancelled','rolled_back')))
);
CREATE UNIQUE INDEX vpn_account_transfers_one_active ON vpn_account_transfers(vpn_account_id) WHERE completed_at IS NULL;
-- Reserve both nodes so another transfer cannot overwrite staged membership.
CREATE TABLE vpn_account_transfer_nodes (
 server_id uuid PRIMARY KEY REFERENCES servers(id),
 transfer_id uuid NOT NULL REFERENCES vpn_account_transfers(id) ON DELETE CASCADE
);

CREATE FUNCTION routegate_transfer_node_guard(p_node uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE op uuid;
BEGIN
 IF p_node IS NULL THEN RETURN; END IF;
 PERFORM id FROM servers WHERE id = p_node FOR UPDATE;
 SELECT transfer_id INTO op FROM vpn_account_transfer_nodes WHERE server_id = p_node;
 IF op IS NOT NULL AND op::text IS DISTINCT FROM current_setting('routegate.transfer_id', true) THEN
  RAISE EXCEPTION USING ERRCODE = 'P0140', MESSAGE = 'node_reserved_for_transfer';
 END IF;
END; $$;

-- Every job insertion shares the same node lock as Start. Completion/heartbeat
-- remain available. Generic apply/rollback/service/update/cleanup cannot race it.
CREATE FUNCTION routegate_transfer_job_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM routegate_transfer_node_guard(NEW.server_id);
 RETURN NEW;
END; $$;
CREATE TRIGGER rg140_config_job BEFORE INSERT ON config_apply_jobs FOR EACH ROW EXECUTE FUNCTION routegate_transfer_job_guard();
CREATE TRIGGER rg140_operation_job BEFORE INSERT ON agent_operation_jobs FOR EACH ROW EXECUTE FUNCTION routegate_transfer_job_guard();
CREATE TRIGGER rg140_update_job BEFORE INSERT ON agent_platform_update_jobs FOR EACH ROW EXECUTE FUNCTION routegate_transfer_job_guard();

CREATE FUNCTION routegate_transfer_account_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE deployed boolean;
BEGIN
 IF TG_OP = 'UPDATE' AND (to_jsonb(OLD) - ARRAY['email','phone','telegram_username','telegram_recipient_id','updated_at','notes']) = (to_jsonb(NEW) - ARRAY['email','phone','telegram_username','telegram_recipient_id','updated_at','notes']) THEN RETURN NEW; END IF;
 IF TG_OP <> 'INSERT' THEN PERFORM routegate_transfer_node_guard(OLD.server_id); END IF;
 IF TG_OP <> 'DELETE' THEN PERFORM routegate_transfer_node_guard(NEW.server_id); END IF;
 IF TG_OP = 'UPDATE' AND NEW.server_id IS DISTINCT FROM OLD.server_id
 AND COALESCE(current_setting('routegate.transfer_id', true), '') = '' THEN
  SELECT EXISTS(SELECT 1 FROM servers s JOIN config_versions cv ON cv.id = s.active_config_version_id
   WHERE s.id = OLD.server_id AND (cv.client_settings->'accounts' ? OLD.id::text OR cv.client_settings->'accounts' IS NULL OR cv.client_settings->'accounts' = 'null'::jsonb)) INTO deployed;
  IF deployed THEN RAISE EXCEPTION USING ERRCODE = 'P0140', MESSAGE = 'staged_transfer_required'; END IF;
 END IF;
 IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER rg140_account BEFORE INSERT OR UPDATE OR DELETE ON vpn_accounts FOR EACH ROW EXECUTE FUNCTION routegate_transfer_account_guard();

CREATE FUNCTION routegate_transfer_preferences_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE account uuid; node uuid;
BEGIN
 -- Agent apply promotes active state; it must not be mistaken for an edit.
 IF TG_OP = 'UPDATE' AND (to_jsonb(OLD) - ARRAY['active_protocol','active_enabled','activated_at','updated_at']) = (to_jsonb(NEW) - ARRAY['active_protocol','active_enabled','activated_at','updated_at']) THEN RETURN NEW; END IF;
 IF TG_TABLE_NAME='vpn_client_profiles' AND TG_OP='INSERT' THEN
 -- GetOrCreate's no-op UPSERT still executes BEFORE INSERT on each refresh.
 IF EXISTS(SELECT 1 FROM vpn_client_profiles WHERE vpn_account_id=NEW.vpn_account_id) THEN RETURN NEW; END IF;
 END IF;
 IF TG_TABLE_NAME = 'vpn_account_protocols' AND TG_OP = 'INSERT' THEN
 IF NOT NEW.desired_explicit THEN RETURN NEW; END IF;
 END IF;
 IF TG_OP = 'DELETE' THEN account := OLD.vpn_account_id; ELSE account := NEW.vpn_account_id; END IF;
 SELECT server_id INTO node FROM vpn_accounts WHERE id = account FOR UPDATE;
 PERFORM routegate_transfer_node_guard(node);
 IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER rg140_profile BEFORE INSERT OR UPDATE OR DELETE ON vpn_client_profiles FOR EACH ROW EXECUTE FUNCTION routegate_transfer_preferences_guard();
CREATE TRIGGER rg140_protocols BEFORE INSERT OR UPDATE OR DELETE ON vpn_account_protocols FOR EACH ROW EXECUTE FUNCTION routegate_transfer_preferences_guard();
CREATE TRIGGER rg140_routing BEFORE INSERT OR UPDATE OR DELETE ON vpn_account_routing_profiles FOR EACH ROW EXECUTE FUNCTION routegate_transfer_preferences_guard();

CREATE FUNCTION routegate_transfer_server_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'DELETE' THEN PERFORM routegate_transfer_node_guard(OLD.id); RETURN OLD; END IF;
 IF (to_jsonb(OLD) - ARRAY['active_config_version_id','updated_at','next_config_version','last_seen_at','vpn_accounts_config_updated_at']) IS DISTINCT FROM (to_jsonb(NEW) - ARRAY['active_config_version_id','updated_at','next_config_version','last_seen_at','vpn_accounts_config_updated_at']) THEN
  PERFORM routegate_transfer_node_guard(OLD.id);
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER rg140_server BEFORE UPDATE OR DELETE ON servers FOR EACH ROW EXECUTE FUNCTION routegate_transfer_server_guard();

-- Preserve active proof despite generic history cleanup and unpin actions.
CREATE FUNCTION routegate_transfer_proof_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM vpn_account_transfers t WHERE t.completed_at IS NULL AND
 ((TG_TABLE_NAME='config_versions' AND OLD.id IN (t.source_version_id,t.target_version_id,t.cleanup_version_id))
 OR (TG_TABLE_NAME='config_apply_jobs' AND OLD.id IN (t.target_job_id,t.cleanup_job_id)))) THEN
  IF TG_OP='DELETE' THEN RAISE EXCEPTION USING ERRCODE='P0140',MESSAGE='transfer_proof_in_use'; END IF;
  IF TG_TABLE_NAME='config_versions' THEN
   IF NEW.rendered_config IS DISTINCT FROM OLD.rendered_config OR NEW.client_settings IS DISTINCT FROM OLD.client_settings OR NEW.config_hash IS DISTINCT FROM OLD.config_hash OR NOT NEW.pinned THEN
    RAISE EXCEPTION USING ERRCODE='P0140',MESSAGE='transfer_proof_in_use';
   END IF;
  END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END; $$;
CREATE TRIGGER rg140_version_proof BEFORE UPDATE OR DELETE ON config_versions FOR EACH ROW EXECUTE FUNCTION routegate_transfer_proof_guard();
CREATE TRIGGER rg140_job_proof BEFORE DELETE ON config_apply_jobs FOR EACH ROW EXECUTE FUNCTION routegate_transfer_proof_guard();

CREATE FUNCTION routegate_transfer_shared_routing_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Shared routing affects whole node configs; reserve its edits while any
 -- transfer is pending. Transactional table locking serializes with Start.
 IF EXISTS(SELECT 1 FROM vpn_account_transfer_nodes) THEN
  RAISE EXCEPTION USING ERRCODE='P0140',MESSAGE='routing_reserved_for_transfer';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END; $$;
CREATE TRIGGER rg140_routing_rules BEFORE INSERT OR UPDATE OR DELETE ON routing_profile_rules FOR EACH ROW EXECUTE FUNCTION routegate_transfer_shared_routing_guard();
CREATE TRIGGER rg140_server_routing BEFORE INSERT OR UPDATE OR DELETE ON server_routing_profiles FOR EACH ROW EXECUTE FUNCTION routegate_transfer_shared_routing_guard();
CREATE TRIGGER rg140_routing_profiles BEFORE INSERT OR UPDATE OR DELETE ON routing_profiles FOR EACH ROW EXECUTE FUNCTION routegate_transfer_shared_routing_guard();

-- A verified canonical cutover changes placement, not the already-rendered
-- credentials. Retain its configuration timestamp across that one transition.
CREATE FUNCTION routegate_transfer_cutover_timestamp() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.server_id IS DISTINCT FROM NEW.server_id
 AND OLD.display_name IS NOT DISTINCT FROM NEW.display_name
 AND OLD.status IS NOT DISTINCT FROM NEW.status
 AND EXISTS(SELECT 1 FROM vpn_account_transfers t WHERE t.vpn_account_id=NEW.id
 AND t.id::text=current_setting('routegate.transfer_id',true) AND t.completed_at IS NULL
 AND NEW.server_id IN (t.source_server_id,t.target_server_id)) THEN
 NEW.config_updated_at:=OLD.config_updated_at;
 END IF;
 RETURN NEW;
END; $$;
-- Alphabetically after the pre-existing timestamp invariant trigger.
CREATE TRIGGER z_rg140_cutover_timestamp BEFORE UPDATE ON vpn_accounts FOR EACH ROW EXECUTE FUNCTION routegate_transfer_cutover_timestamp();

CREATE FUNCTION routegate_transfer_manager_update_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.operation='apply' THEN
  PERFORM pg_advisory_xact_lock(140159);
  IF EXISTS(SELECT 1 FROM vpn_account_transfer_nodes) THEN
   RAISE EXCEPTION USING ERRCODE='P0140',MESSAGE='manager_update_reserved_for_transfer';
  END IF;
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER rg140_manager_update BEFORE INSERT ON update_jobs FOR EACH ROW EXECUTE FUNCTION routegate_transfer_manager_update_guard();
