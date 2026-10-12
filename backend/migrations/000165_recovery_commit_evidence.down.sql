DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM runtime_recovery_commit_evidence) THEN
 RAISE EXCEPTION 'recovery_commit_evidence_retained'; END IF;
END; $$;
DROP TABLE runtime_recovery_commit_evidence;
DROP FUNCTION routegate_recovery_commit_evidence_guard();
