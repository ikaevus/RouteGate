DO $$ BEGIN
 LOCK TABLE runtime_recovery_cleaned_commit_evidence IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM runtime_recovery_cleaned_commit_evidence) THEN
 RAISE EXCEPTION 'recovery_cleaned_commit_evidence_retained'; END IF;
 DROP TABLE runtime_recovery_cleaned_commit_evidence;
 DROP FUNCTION routegate_recovery_cleaned_commit_evidence_guard();
 DROP FUNCTION routegate_recovery_cleaned_commit_wire(json);
END; $$;
