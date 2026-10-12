-- Historical authenticated read-only evidence. Never an unlock grant.
CREATE TABLE runtime_recovery_observation_evidence (
 preparation_id uuid PRIMARY KEY REFERENCES runtime_recovery_observation_approvals(preparation_id),
 challenge_hash text NOT NULL CHECK(challenge_hash ~ '^[0-9a-f]{64}$'),
 response text NOT NULL CHECK(octet_length(response) BETWEEN 1 AND 8192),
 response_hash text NOT NULL CHECK(response_hash ~ '^[0-9a-f]{64}$'),
 observed_at timestamptz NOT NULL,
 accepted_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE FUNCTION routegate_recovery_observation_evidence_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE node uuid; prep runtime_recovery_preparations; approval runtime_recovery_observation_approvals; data jsonb;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION USING ERRCODE='P0143',MESSAGE='recovery_evidence_retained'; END IF;
 SELECT server_id INTO node FROM runtime_recovery_preparations WHERE id=NEW.preparation_id;
 PERFORM id FROM servers WHERE id=node FOR UPDATE;
 SELECT * INTO prep FROM runtime_recovery_preparations WHERE id=NEW.preparation_id FOR UPDATE;
 SELECT * INTO approval FROM runtime_recovery_observation_approvals WHERE preparation_id=NEW.preparation_id;
 PERFORM id FROM agents WHERE id=prep.agent_id FOR UPDATE;
 data:=NEW.response::jsonb;
 IF prep.id IS NULL OR prep.state<>'prepared' OR approval.preparation_id IS NULL
 OR clock_timestamp()<approval.issued_at OR clock_timestamp()>=approval.expires_at
 OR NEW.observed_at<approval.issued_at OR NEW.observed_at>=approval.expires_at OR NEW.observed_at>clock_timestamp()
 OR NEW.challenge_hash<>approval.challenge_hash
 OR NEW.response_hash<>encode(digest(NEW.response,'sha256'),'hex')
 OR NOT COALESCE(data->>'schemaVersion'='1' AND data->>'operationId'=prep.id::text
 AND data->>'challengeHash'=approval.challenge_hash AND (data->>'observedAt')::timestamptz=NEW.observed_at
 AND data->'observation'=(approval.challenge::jsonb)->'expected',false) THEN
 RAISE EXCEPTION USING ERRCODE='P0143',MESSAGE='recovery_evidence_binding_mismatch'; END IF;
 IF NOT EXISTS(SELECT 1 FROM agents WHERE id=prep.agent_id AND server_id=prep.server_id
 AND credential_generation=prep.agent_credential_generation AND status='online'
 AND current_runtime_mutation_fencing AND capabilities @> '{"runtimeMutationFencingV1":true}'::jsonb
 AND capabilities @> '{"runtimeRecoveryObservationV1":true}'::jsonb
 AND last_authenticated_heartbeat_generation=credential_generation
 AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds') THEN
 RAISE EXCEPTION USING ERRCODE='P0143',MESSAGE='recovery_agent_not_ready'; END IF;
 NEW.accepted_at:=clock_timestamp();
 RETURN NEW;
END; $$;
CREATE TRIGGER rg144_evidence BEFORE INSERT OR UPDATE OR DELETE ON runtime_recovery_observation_evidence FOR EACH ROW EXECUTE FUNCTION routegate_recovery_observation_evidence_guard();
