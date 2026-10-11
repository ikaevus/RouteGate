DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM runtime_recovery_observation_approvals) THEN RAISE EXCEPTION 'recovery_observation_approval_history_retained'; END IF;
END; $$;
DROP TABLE runtime_recovery_observation_approvals;
DROP FUNCTION routegate_recovery_observation_approval_guard();
