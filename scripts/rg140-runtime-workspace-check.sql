-- CI-only readback through the same restricted login used by the Manager.
-- The containing workflow uses a fresh PostgreSQL service per matrix leg.
DO $check$
DECLARE
  elevated boolean;
BEGIN
  IF current_database() <> 'routegate_workspace_e2e'
     OR current_user <> 'rg140_workspace_runtime' THEN
    RAISE EXCEPTION 'runtime workspace fixture identity mismatch';
  END IF;
  SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolinherit OR rolreplication
    OR rolbypassrls OR EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid)
    INTO elevated FROM pg_roles r WHERE rolname=current_user;
  IF elevated OR has_schema_privilege(current_user,'public','CREATE')
     OR has_database_privilege(current_user,current_database(),'CREATE')
     OR has_table_privilege(current_user,'public.schema_migrations','INSERT,UPDATE,DELETE,TRUNCATE,TRIGGER')
     OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
               WHERE n.nspname='public' AND c.relowner=(SELECT oid FROM pg_roles WHERE rolname=current_user)) THEN
    RAISE EXCEPTION 'runtime login acquired elevated authority';
  END IF;
  IF NOT EXISTS(SELECT 1 FROM users)
     OR NOT EXISTS(SELECT 1 FROM auth_sessions)
     OR NOT EXISTS(SELECT 1 FROM vpn_accounts)
     OR NOT EXISTS(SELECT 1 FROM routing_profiles) THEN
    RAISE EXCEPTION 'full Manager workspace writes were not exercised';
  END IF;
  -- These subtransactions must fail specifically on privilege checks. Any
  -- unexpected success raises a different error and fails the CI step.
  BEGIN
    DELETE FROM schema_migrations;
    RAISE EXCEPTION 'runtime can mutate migration ledger';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    ALTER TABLE vpn_credential_revocations DISABLE TRIGGER ALL;
    RAISE EXCEPTION 'runtime can disable revocation triggers';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    SET LOCAL ROLE routegate;
    RAISE EXCEPTION 'runtime can assume migration owner';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
END
$check$;
