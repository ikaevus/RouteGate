DO $$ BEGIN
 LOCK TABLE runtime_recovery_release_authorizations IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM runtime_recovery_release_authorizations) THEN
 RAISE EXCEPTION 'recovery_release_authorization_retained'; END IF;
 DROP TABLE runtime_recovery_release_authorizations;
 DROP FUNCTION routegate_recovery_release_authorization_guard();
END; $$;
