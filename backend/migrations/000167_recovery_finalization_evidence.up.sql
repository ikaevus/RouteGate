-- Historical final-consumption evidence only; no release authority or unlock.
CREATE TABLE runtime_recovery_finalization_evidence (
 preparation_id uuid PRIMARY KEY REFERENCES runtime_recovery_release_authorizations(preparation_id),
 authorization_hash text NOT NULL CHECK(authorization_hash ~ '^[0-9a-f]{64}$'),
 final_consumption_hash text NOT NULL UNIQUE CHECK(final_consumption_hash ~ '^[0-9a-f]{64}$'),
 proof text NOT NULL CHECK(octet_length(proof) BETWEEN 1 AND 8192),
 proof_hash text NOT NULL CHECK(proof_hash ~ '^[0-9a-f]{64}$'),
 observed_at timestamptz NOT NULL,
 accepted_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE FUNCTION routegate_recovery_finalization_evidence_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE node uuid; prep runtime_recovery_preparations; authority runtime_recovery_release_authorizations; data jsonb;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION USING ERRCODE='P0147',MESSAGE='recovery_finalization_evidence_retained'; END IF;
 SELECT server_id INTO node FROM runtime_recovery_preparations WHERE id=NEW.preparation_id;
 PERFORM id FROM servers WHERE id=node FOR UPDATE;
 SELECT * INTO prep FROM runtime_recovery_preparations WHERE id=NEW.preparation_id FOR UPDATE;
 SELECT * INTO authority FROM runtime_recovery_release_authorizations WHERE preparation_id=NEW.preparation_id;
 PERFORM id FROM agents WHERE id=prep.agent_id FOR UPDATE;
 LOCK TABLE user_roles,roles,role_permissions,permissions IN SHARE MODE;
 PERFORM u.id FROM users u JOIN auth_sessions s ON s.user_id=u.id
 WHERE u.id=authority.actor_user_id AND s.id=authority.actor_session_id FOR SHARE OF u,s;
 data:=NEW.proof::jsonb;
 IF prep.id IS NULL OR prep.state<>'prepared' OR authority.preparation_id IS NULL
 OR clock_timestamp()<authority.issued_at OR clock_timestamp()>=authority.expires_at
 OR NEW.observed_at<authority.issued_at OR NEW.observed_at>=authority.expires_at OR NEW.observed_at>clock_timestamp()
 OR NEW.authorization_hash<>authority.authorization_hash
 OR NEW.proof_hash<>encode(digest(NEW.proof,'sha256'),'hex')
 OR NOT COALESCE(jsonb_typeof(data)='object' AND data->'schemaVersion'='1'::jsonb
 AND data->>'operationId'=prep.id::text AND data->>'authorizationHash'=NEW.authorization_hash
 AND data->>'finalConsumptionHash'=NEW.final_consumption_hash AND (data->>'observedAt')::timestamptz=NEW.observed_at
 AND data->'observation'=(authority.authorization_payload::jsonb)->'expected'
 AND data-ARRAY['schemaVersion','operationId','authorizationHash','finalConsumptionHash','observedAt','observation']='{}'::jsonb,false) THEN
 RAISE EXCEPTION USING ERRCODE='P0147',MESSAGE='recovery_finalization_binding_mismatch'; END IF;
 IF NOT EXISTS(SELECT 1 FROM agents WHERE id=prep.agent_id AND server_id=prep.server_id
 AND credential_generation=prep.agent_credential_generation AND status='online'
 AND current_runtime_mutation_fencing
 AND capabilities @> '{"runtimeMutationFencingV1":true,"runtimeRecoveryObservationV1":true,"runtimeRecoveryGrantReadV1":true}'::jsonb
 AND last_authenticated_heartbeat_generation=credential_generation
 AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds') THEN
 RAISE EXCEPTION USING ERRCODE='P0147',MESSAGE='recovery_finalization_agent_not_ready'; END IF;
 IF NOT EXISTS(SELECT 1 FROM users u JOIN auth_sessions s ON s.user_id=u.id
 WHERE u.id=authority.actor_user_id AND s.id=authority.actor_session_id AND u.status='active' AND u.user_type='human'
 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.expires_at>=authority.expires_at
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=u.id AND r.code IN ('admin','super_admin'))
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id JOIN permissions p ON p.id=rp.permission_id WHERE ur.user_id=u.id AND p.code='configs:apply')) THEN
 RAISE EXCEPTION USING ERRCODE='P0147',MESSAGE='recovery_finalization_forbidden'; END IF;
 NEW.accepted_at:=clock_timestamp();
 RETURN NEW;
END; $$;
CREATE TRIGGER rg148_finalization_evidence BEFORE INSERT OR UPDATE OR DELETE ON runtime_recovery_finalization_evidence FOR EACH ROW EXECUTE FUNCTION routegate_recovery_finalization_evidence_guard();
