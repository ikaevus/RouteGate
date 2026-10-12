-- Internal issuance only. No transport, consumption or Agent fence release.
CREATE TABLE runtime_recovery_mutation_grants (
 preparation_id uuid PRIMARY KEY REFERENCES runtime_recovery_observation_evidence(preparation_id),
 actor_user_id uuid NOT NULL,
 actor_session_id uuid NOT NULL,
 grant_payload text NOT NULL CHECK(octet_length(grant_payload) BETWEEN 1 AND 8192),
 grant_hash text NOT NULL CHECK(grant_hash ~ '^[0-9a-f]{64}$'),
 issued_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>issued_at AND expires_at<=issued_at+interval '2 minutes')
);
CREATE UNIQUE INDEX runtime_recovery_grant_nonce ON runtime_recovery_mutation_grants ((grant_payload::jsonb->>'nonce'));
CREATE FUNCTION routegate_recovery_mutation_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE node uuid; prep runtime_recovery_preparations; approval runtime_recovery_observation_approvals; evidence runtime_recovery_observation_evidence; data jsonb;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION USING ERRCODE='P0144',MESSAGE='recovery_grant_retained'; END IF;
 SELECT server_id INTO node FROM runtime_recovery_preparations WHERE id=NEW.preparation_id;
 PERFORM id FROM servers WHERE id=node FOR UPDATE;
 SELECT * INTO prep FROM runtime_recovery_preparations WHERE id=NEW.preparation_id FOR UPDATE;
 SELECT * INTO approval FROM runtime_recovery_observation_approvals WHERE preparation_id=NEW.preparation_id;
 SELECT * INTO evidence FROM runtime_recovery_observation_evidence WHERE preparation_id=NEW.preparation_id;
 PERFORM id FROM agents WHERE id=prep.agent_id FOR UPDATE;
 data:=NEW.grant_payload::jsonb;
 IF prep.id IS NULL OR prep.state<>'prepared' OR approval.preparation_id IS NULL OR evidence.preparation_id IS NULL
 OR NEW.actor_user_id<>approval.actor_user_id
 OR NEW.issued_at<approval.issued_at OR NEW.issued_at<evidence.accepted_at OR NEW.issued_at>clock_timestamp()
 OR NEW.expires_at<>approval.expires_at OR NEW.expires_at<=clock_timestamp()
 OR NEW.grant_hash<>encode(digest(NEW.grant_payload,'sha256'),'hex')
 OR NOT COALESCE(data->>'schemaVersion'='1' AND data->>'purpose'='runtime_mutation_fence_release_v1'
 AND data->>'operationId'=prep.id::text AND data->>'nonce' ~ '^[0-9a-f]{64}$'
 AND (data->>'agentCredentialGeneration')::bigint=prep.agent_credential_generation
 AND data->>'challengeHash'=approval.challenge_hash AND data->>'evidenceHash'=evidence.response_hash
 AND (data->>'issuedAt')::timestamptz=NEW.issued_at AND (data->>'expiresAt')::timestamptz=NEW.expires_at
 AND data->'expected'=(evidence.response::jsonb)->'observation',false) THEN
 RAISE EXCEPTION USING ERRCODE='P0144',MESSAGE='recovery_grant_binding_mismatch'; END IF;
 IF NOT EXISTS(SELECT 1 FROM agents WHERE id=prep.agent_id AND server_id=prep.server_id
 AND credential_generation=prep.agent_credential_generation AND status='online'
 AND current_runtime_mutation_fencing AND capabilities @> '{"runtimeMutationFencingV1":true,"runtimeRecoveryObservationV1":true}'::jsonb
 AND last_authenticated_heartbeat_generation=credential_generation
 AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds') THEN
 RAISE EXCEPTION USING ERRCODE='P0144',MESSAGE='recovery_grant_agent_not_ready'; END IF;
 IF NOT EXISTS(SELECT 1 FROM users u JOIN auth_sessions s ON s.user_id=u.id
 WHERE u.id=NEW.actor_user_id AND s.id=NEW.actor_session_id AND u.status='active' AND u.user_type='human'
 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=u.id AND r.code IN ('admin','super_admin'))
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id JOIN permissions p ON p.id=rp.permission_id WHERE ur.user_id=u.id AND p.code='configs:apply')) THEN
 RAISE EXCEPTION USING ERRCODE='P0144',MESSAGE='recovery_grant_forbidden'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER rg145_grant BEFORE INSERT OR UPDATE OR DELETE ON runtime_recovery_mutation_grants FOR EACH ROW EXECUTE FUNCTION routegate_recovery_mutation_grant_guard();

-- Once authorization exists, reservation release requires a future explicit
-- abort/reconciliation protocol. Expiry is not proof that no Agent used it.
CREATE FUNCTION routegate_recovery_granted_reservation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state IS DISTINCT FROM OLD.state AND EXISTS(SELECT 1 FROM runtime_recovery_mutation_grants WHERE preparation_id=OLD.id) THEN
 RAISE EXCEPTION USING ERRCODE='P0144',MESSAGE='recovery_grant_reservation_retained'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER rg145_granted_reservation BEFORE UPDATE ON runtime_recovery_preparations FOR EACH ROW EXECUTE FUNCTION routegate_recovery_granted_reservation_guard();
