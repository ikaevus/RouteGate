DO $$ BEGIN
 LOCK TABLE runtime_recovery_prepared_release_evidence IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM runtime_recovery_prepared_release_evidence) THEN
 RAISE EXCEPTION 'recovery_prepared_release_evidence_retained'; END IF;
 DROP TABLE runtime_recovery_prepared_release_evidence;
 DROP FUNCTION routegate_recovery_prepared_release_evidence_guard();
 DROP FUNCTION routegate_recovery_prepared_release_wire(json);
END; $$;
