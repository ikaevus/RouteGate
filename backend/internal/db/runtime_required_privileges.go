package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CheckRuntimeRequiredPrivileges checks the minimum schema-175 application
// grants through the serving connection, using SELECT only. A restored migration
// ledger does not prove that pg_restore --no-privileges restored these grants.
// This neither repairs ACLs nor attests absence of excessive privileges. Runtime
// ownership is checked separately, before this check, by PrepareSchema.
func CheckRuntimeRequiredPrivileges(ctx context.Context, pool *pgxpool.Pool) error {
	var ready bool
	err := pool.QueryRow(ctx, `SELECT
 pg_catalog.has_database_privilege(current_database(), 'CONNECT')
 AND pg_catalog.has_schema_privilege('public', 'USAGE')
 AND pg_catalog.has_table_privilege('public.schema_migrations', 'SELECT')
 AND NOT EXISTS (
   SELECT 1 FROM unnest($1::text[]) AS required(name)
   CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE']) AS needed(privilege)
   LEFT JOIN pg_catalog.pg_namespace n ON n.nspname='public'
   LEFT JOIN pg_catalog.pg_class c ON c.relnamespace=n.oid AND c.relname=required.name
   WHERE c.oid IS NULL OR c.relkind NOT IN ('r','p')
      OR NOT pg_catalog.has_table_privilege(c.oid, needed.privilege)
 )
 AND NOT EXISTS (
   SELECT 1 FROM pg_catalog.pg_class s
   JOIN pg_catalog.pg_depend d ON d.classid='pg_class'::regclass AND d.objid=s.oid
   JOIN pg_catalog.pg_class t ON t.oid=d.refobjid
   JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace
   CROSS JOIN unnest(ARRAY['USAGE','SELECT']) AS needed(privilege)
   WHERE s.relkind='S' AND d.refclassid='pg_class'::regclass
     AND d.deptype IN ('a','i') AND n.nspname='public' AND t.relname=ANY($1::text[])
     AND NOT pg_catalog.has_sequence_privilege(s.oid, needed.privilege)
 )`, runtimeTables).Scan(&ready)
	if err != nil {
		return fmt.Errorf("runtime required privileges check: %w", err)
	}
	if !ready {
		return fmt.Errorf("external migration mode requires complete runtime privileges; administrative reconciliation required")
	}
	return nil
}
