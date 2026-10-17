DO $$ BEGIN
 LOCK TABLE runtime_recovery_retention_evidence IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM runtime_recovery_retention_evidence) THEN
 RAISE EXCEPTION 'recovery_retention_evidence_retained'; END IF;
 DROP TABLE runtime_recovery_retention_evidence;
 DROP FUNCTION routegate_recovery_retention_evidence_guard();
 DROP FUNCTION routegate_recovery_retention_wire(json);
END; $$;
