-- Approval to collect read-only evidence, never to unlock or change runtime.
CREATE TABLE runtime_recovery_observation_approvals (
 preparation_id uuid PRIMARY KEY REFERENCES runtime_recovery_preparations(id),
 actor_user_id uuid NOT NULL,
 actor_session_id uuid NOT NULL,
 challenge text NOT NULL CHECK(octet_length(challenge) BETWEEN 1 AND 8192),
 challenge_hash text NOT NULL CHECK(challenge_hash ~ '^[0-9a-f]{64}$'),
 issued_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>issued_at AND expires_at<=issued_at+interval '2 minutes')
);
CREATE FUNCTION routegate_recovery_observation_approval_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE node uuid; prep runtime_recovery_preparations; data jsonb;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='recovery_observation_approval_retained'; END IF;
 SELECT server_id INTO node FROM runtime_recovery_preparations WHERE id=NEW.preparation_id;
 PERFORM id FROM servers WHERE id=node FOR UPDATE;
 SELECT * INTO prep FROM runtime_recovery_preparations WHERE id=NEW.preparation_id FOR UPDATE;
 IF prep.id IS NULL OR prep.state<>'prepared' THEN RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='recovery_preparation_not_active'; END IF;
 data:=NEW.challenge::jsonb;
 IF NEW.issued_at>clock_timestamp() OR NEW.expires_at<=clock_timestamp()
 OR NEW.challenge_hash<>encode(digest(NEW.challenge,'sha256'),'hex')
 OR NOT COALESCE(data->>'schemaVersion'='1' AND data->>'operationId'=prep.id::text
 AND data->>'nonce' ~ '^[0-9a-f]{64}$'
 AND (data->>'issuedAt')::timestamptz=NEW.issued_at AND (data->>'expiresAt')::timestamptz=NEW.expires_at
 AND data->'expected'->>'taskId'=prep.task_id::text
 AND data->'expected'->>'agentId'=prep.agent_id::text AND data->'expected'->>'serverId'=prep.server_id::text
 AND data->'expected'->>'markerHash'=prep.marker_hash AND data->'expected'->>'receiptHash'=prep.receipt_hash
 AND data->'expected'->>'envelopeHash'=prep.envelope_hash,false) THEN
 RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='recovery_observation_binding_mismatch'; END IF;
 IF NOT EXISTS(SELECT 1 FROM agents WHERE id=prep.agent_id AND server_id=prep.server_id
 AND credential_generation=prep.agent_credential_generation AND status='online'
 AND current_runtime_mutation_fencing AND capabilities @> '{"runtimeMutationFencingV1":true}'::jsonb
 AND last_authenticated_heartbeat_generation=credential_generation
 AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds') THEN
 RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='recovery_agent_not_ready'; END IF;
 -- The service locks these authority tables while checking/creating approval.
 IF NOT EXISTS(SELECT 1 FROM users u JOIN auth_sessions s ON s.user_id=u.id
 WHERE u.id=NEW.actor_user_id AND s.id=NEW.actor_session_id AND u.status='active' AND u.user_type='human'
 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=u.id AND r.code IN ('admin','super_admin'))
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id JOIN permissions p ON p.id=rp.permission_id WHERE ur.user_id=u.id AND p.code='configs:apply')) THEN
 RAISE EXCEPTION USING ERRCODE='P0142',MESSAGE='recovery_approval_forbidden'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER rg143_approval BEFORE INSERT OR UPDATE OR DELETE ON runtime_recovery_observation_approvals FOR EACH ROW EXECUTE FUNCTION routegate_recovery_observation_approval_guard();
