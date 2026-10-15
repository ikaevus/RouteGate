-- Authenticated archival statement only: no fence/reservation release.
CREATE TABLE runtime_recovery_terminal_evidence (
 preparation_id uuid PRIMARY KEY REFERENCES runtime_recovery_finalization_evidence(preparation_id),
 proof text NOT NULL CHECK(octet_length(proof) BETWEEN 1 AND 8192),
 proof_hash text NOT NULL CHECK(proof_hash ~ '^[0-9a-f]{64}$'),
 observed_at timestamptz NOT NULL,
 accepted_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE FUNCTION routegate_recovery_terminal_wire(value json) RETURNS text LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE result text; kind text:=json_typeof(value);
BEGIN
 IF kind='object' THEN
 IF EXISTS(SELECT 1 FROM json_each(value) GROUP BY key HAVING count(*)>1) THEN
 RAISE EXCEPTION USING ERRCODE='P0148',MESSAGE='recovery_terminal_duplicate_key'; END IF;
 SELECT '{'||COALESCE(string_agg(to_json(key)::text||':'||routegate_recovery_terminal_wire(v),',' ORDER BY array_position(ARRAY['schemaVersion','purpose','operationId','agentCredentialGeneration','authorizationHash','finalConsumptionHash','finalProofHash','finalReceiptHash','intentHash','terminalHash','observedAt','observation','taskId','agentId','serverId','markerHash','receiptHash','envelopeHash','witness','runtimeHash','generation','previousGeneration','listenerPort','invocationId','pid','startedMonotonic'],key),key),'')||'}' INTO result FROM json_each(value) AS fields(key,v);
 RETURN result;
 ELSIF kind='string' THEN RETURN to_json(value#>>'{}')::text;
 ELSIF kind='number' THEN RETURN trim_scale(value::text::numeric)::text;
 ELSE RAISE EXCEPTION USING ERRCODE='P0148',MESSAGE='recovery_terminal_invalid_wire';
 END IF;
END; $$;

CREATE FUNCTION routegate_recovery_terminal_evidence_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE node uuid; prep runtime_recovery_preparations; authority runtime_recovery_release_authorizations; final runtime_recovery_finalization_evidence; data jsonb; ack text;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION USING ERRCODE='P0148',MESSAGE='recovery_terminal_evidence_retained'; END IF;
 SELECT server_id INTO node FROM runtime_recovery_preparations WHERE id=NEW.preparation_id;
 PERFORM id FROM servers WHERE id=node FOR UPDATE;
 SELECT * INTO prep FROM runtime_recovery_preparations WHERE id=NEW.preparation_id FOR UPDATE;
 SELECT * INTO authority FROM runtime_recovery_release_authorizations WHERE preparation_id=NEW.preparation_id;
 SELECT * INTO final FROM runtime_recovery_finalization_evidence WHERE preparation_id=NEW.preparation_id;
 PERFORM id FROM agents WHERE id=prep.agent_id FOR UPDATE;
 LOCK TABLE user_roles,roles,role_permissions,permissions IN SHARE MODE;
 PERFORM u.id FROM users u JOIN auth_sessions s ON s.user_id=u.id
 WHERE u.id=authority.actor_user_id AND s.id=authority.actor_session_id FOR SHARE OF u,s;
 IF NEW.proof<>routegate_recovery_terminal_wire(NEW.proof::json) THEN
 RAISE EXCEPTION USING ERRCODE='P0148',MESSAGE='recovery_terminal_noncanonical_wire'; END IF;
 data:=NEW.proof::jsonb;
 -- Exact canonical receipt pinned by Agent for the original canonical final proof.
 ack:='{"schemaVersion":1,"recorded":true,"operationId":'||to_json(prep.id::text)::text||
 ',"authorizationHash":'||to_json(final.authorization_hash)::text||
 ',"finalConsumptionHash":'||to_json(final.final_consumption_hash)::text||
 ',"proofHash":'||to_json(final.proof_hash)::text||',"requestHash":'||to_json(final.proof_hash)::text||'}';
 IF prep.id IS NULL OR prep.state<>'prepared' OR authority.preparation_id IS NULL OR final.preparation_id IS NULL
 OR clock_timestamp()<authority.issued_at OR clock_timestamp()>=authority.expires_at
 OR NEW.observed_at<authority.issued_at OR NEW.observed_at<final.observed_at
 OR NEW.observed_at>=authority.expires_at OR NEW.observed_at>clock_timestamp()
 OR NEW.proof_hash<>encode(digest(NEW.proof,'sha256'),'hex')
 OR NOT COALESCE(jsonb_typeof(data)='object' AND data->'schemaVersion'='1'::jsonb
 AND data->>'purpose'='runtime_mutation_retirement_evidence_v1'
 AND data->>'operationId'=prep.id::text
 AND data->'agentCredentialGeneration'=to_jsonb(prep.agent_credential_generation)
 AND data->>'authorizationHash'=authority.authorization_hash
 AND data->>'authorizationHash'=final.authorization_hash
 AND data->>'finalConsumptionHash'=final.final_consumption_hash
 AND data->>'finalProofHash'=final.proof_hash
 AND data->>'finalReceiptHash'=encode(digest(ack,'sha256'),'hex')
 AND data->>'intentHash' ~ '^[0-9a-f]{64}$' AND data->>'terminalHash' ~ '^[0-9a-f]{64}$'
 AND (data->>'observedAt')::timestamptz=NEW.observed_at
 AND data->>'observedAt' ~ '^[0-9]{4}-(0[1-9]|1[0-2])-([0-2][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{0,8}[1-9])?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$'
 AND data->>'observedAt' !~ '[+-]00:00$'
 AND data->'observation'=(authority.authorization_payload::jsonb)->'expected'
 AND data->'observation'=final.proof::jsonb->'observation'
 AND data-ARRAY['schemaVersion','purpose','operationId','agentCredentialGeneration','authorizationHash','finalConsumptionHash','finalProofHash','finalReceiptHash','intentHash','terminalHash','observedAt','observation']='{}'::jsonb,false) THEN
 RAISE EXCEPTION USING ERRCODE='P0148',MESSAGE='recovery_terminal_binding_mismatch'; END IF;
 IF NOT EXISTS(SELECT 1 FROM agents WHERE id=prep.agent_id AND server_id=prep.server_id
 AND credential_generation=prep.agent_credential_generation AND status='online'
 AND current_runtime_mutation_fencing
 AND capabilities @> '{"runtimeMutationFencingV1":true,"runtimeRecoveryObservationV1":true,"runtimeRecoveryGrantReadV1":true}'::jsonb
 AND last_authenticated_heartbeat_generation=credential_generation
 AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds') THEN
 RAISE EXCEPTION USING ERRCODE='P0148',MESSAGE='recovery_terminal_agent_not_ready'; END IF;
 IF NOT EXISTS(SELECT 1 FROM users u JOIN auth_sessions s ON s.user_id=u.id
 WHERE u.id=authority.actor_user_id AND s.id=authority.actor_session_id AND u.status='active' AND u.user_type='human'
 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.expires_at>=authority.expires_at
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=u.id AND r.code IN ('admin','super_admin'))
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id JOIN permissions p ON p.id=rp.permission_id WHERE ur.user_id=u.id AND p.code='configs:apply')) THEN
 RAISE EXCEPTION USING ERRCODE='P0148',MESSAGE='recovery_terminal_forbidden'; END IF;
 NEW.accepted_at:=clock_timestamp();
 RETURN NEW;
END; $$;
CREATE TRIGGER rg149_terminal_evidence BEFORE INSERT OR UPDATE OR DELETE ON runtime_recovery_terminal_evidence FOR EACH ROW EXECUTE FUNCTION routegate_recovery_terminal_evidence_guard();
