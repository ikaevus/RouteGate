DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM runtime_recovery_mutation_grants) THEN RAISE EXCEPTION 'recovery_grant_history_retained'; END IF;
END; $$;
DROP TRIGGER rg145_granted_reservation ON runtime_recovery_preparations;
DROP FUNCTION routegate_recovery_granted_reservation_guard();
DROP TABLE runtime_recovery_mutation_grants;
DROP FUNCTION routegate_recovery_mutation_grant_guard();
