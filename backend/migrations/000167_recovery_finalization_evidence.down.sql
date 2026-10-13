DO $$ BEGIN
 LOCK TABLE runtime_recovery_finalization_evidence IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM runtime_recovery_finalization_evidence) THEN
 RAISE EXCEPTION 'recovery_finalization_evidence_retained'; END IF;
 DROP TABLE runtime_recovery_finalization_evidence;
 DROP FUNCTION routegate_recovery_finalization_evidence_guard();
END; $$;
