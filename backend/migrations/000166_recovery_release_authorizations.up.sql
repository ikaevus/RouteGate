-- Dormant conditional final authorization only. No delivery or Agent unlock.
CREATE TABLE runtime_recovery_release_authorizations (
 preparation_id uuid PRIMARY KEY REFERENCES runtime_recovery_commit_evidence(preparation_id),
 actor_user_id uuid NOT NULL,
 actor_session_id uuid NOT NULL,
 authorization text NOT NULL CHECK(octet_length(authorization) BETWEEN 1 AND 8192),
 authorization_hash text NOT NULL UNIQUE CHECK(authorization_hash ~ '^[0-9a-f]{64}$'),
 issued_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>issued_at AND expires_at<=issued_at+interval '2 minutes')
);
CREATE UNIQUE INDEX runtime_recovery_release_nonce ON runtime_recovery_release_authorizations ((authorization::jsonb->>'nonce'));
CREATE FUNCTION routegate_recovery_release_authorization_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE node uuid; prep runtime_recovery_preparations; evidence runtime_recovery_commit_evidence; grant_record runtime_recovery_mutation_grants; data jsonb;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION USING ERRCODE='P0146',MESSAGE='recovery_release_authorization_retained'; END IF;
 SELECT server_id INTO node FROM runtime_recovery_preparations WHERE id=NEW.preparation_id;
 PERFORM id FROM servers WHERE id=node FOR UPDATE;
 SELECT * INTO prep FROM runtime_recovery_preparations WHERE id=NEW.preparation_id FOR UPDATE;
 SELECT * INTO evidence FROM runtime_recovery_commit_evidence WHERE preparation_id=NEW.preparation_id;
 SELECT * INTO grant_record FROM runtime_recovery_mutation_grants WHERE preparation_id=NEW.preparation_id;
 PERFORM id FROM agents WHERE id=prep.agent_id FOR UPDATE;
 LOCK TABLE user_roles,roles,role_permissions,permissions IN SHARE MODE;
 PERFORM u.id FROM users u JOIN auth_sessions s ON s.user_id=u.id
 WHERE u.id=NEW.actor_user_id AND s.id=NEW.actor_session_id FOR SHARE OF u,s;
 data:=NEW.authorization::jsonb;
 IF prep.id IS NULL OR prep.state<>'prepared' OR evidence.preparation_id IS NULL OR grant_record.preparation_id IS NULL
 OR evidence.grant_hash<>grant_record.grant_hash
 OR NEW.issued_at<evidence.accepted_at OR NEW.issued_at<evidence.observed_at OR NEW.issued_at>clock_timestamp()
 OR NEW.expires_at<=clock_timestamp()
 OR NEW.authorization_hash<>encode(digest(NEW.authorization,'sha256'),'hex')
 OR NOT COALESCE(jsonb_typeof(data)='object' AND data->'schemaVersion'='1'::jsonb
 AND data->>'purpose'='runtime_mutation_fence_final_release_v1'
 AND data->>'operationId'=prep.id::text AND data->>'nonce' ~ '^[0-9a-f]{64}$'
 AND data->>'nonce'<>(grant_record.grant_payload::jsonb->>'nonce')
 AND data->'agentCredentialGeneration'=to_jsonb(prep.agent_credential_generation)
 AND data->>'grantHash'=evidence.grant_hash AND data->>'consumptionHash'=evidence.consumption_hash
 AND data->>'commitProofHash'=evidence.proof_hash
 AND (data->>'issuedAt')::timestamptz=NEW.issued_at AND (data->>'expiresAt')::timestamptz=NEW.expires_at
 AND data->'expected'=(evidence.proof::jsonb)->'observation'
 AND data-ARRAY['schemaVersion','purpose','operationId','nonce','agentCredentialGeneration','grantHash','consumptionHash','commitProofHash','issuedAt','expiresAt','expected']='{}'::jsonb,false) THEN
 RAISE EXCEPTION USING ERRCODE='P0146',MESSAGE='recovery_release_binding_mismatch'; END IF;
 IF NOT EXISTS(SELECT 1 FROM agents WHERE id=prep.agent_id AND server_id=prep.server_id
 AND credential_generation=prep.agent_credential_generation AND status='online'
 AND current_runtime_mutation_fencing
 AND capabilities @> '{"runtimeMutationFencingV1":true,"runtimeRecoveryObservationV1":true,"runtimeRecoveryGrantReadV1":true}'::jsonb
 AND last_authenticated_heartbeat_generation=credential_generation
 AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds') THEN
 RAISE EXCEPTION USING ERRCODE='P0146',MESSAGE='recovery_release_agent_not_ready'; END IF;
 IF NOT EXISTS(SELECT 1 FROM users u JOIN auth_sessions s ON s.user_id=u.id
 WHERE u.id=NEW.actor_user_id AND s.id=NEW.actor_session_id AND u.status='active' AND u.user_type='human'
 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND NEW.expires_at<=s.expires_at
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=u.id AND r.code IN ('admin','super_admin'))
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id JOIN permissions p ON p.id=rp.permission_id WHERE ur.user_id=u.id AND p.code='configs:apply')) THEN
 RAISE EXCEPTION USING ERRCODE='P0146',MESSAGE='recovery_release_forbidden'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER rg147_release_authorization BEFORE INSERT OR UPDATE OR DELETE ON runtime_recovery_release_authorizations FOR EACH ROW EXECUTE FUNCTION routegate_recovery_release_authorization_guard();
