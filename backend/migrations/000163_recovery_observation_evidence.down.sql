DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM runtime_recovery_observation_evidence) THEN
 RAISE EXCEPTION 'recovery_evidence_retained'; END IF;
END; $$;
DROP TABLE runtime_recovery_observation_evidence;
DROP FUNCTION routegate_recovery_observation_evidence_guard();
