package db

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRuntimeRoleRequiredPrivileges(t *testing.T) {
	f := newDispatchGateFixture(t)
	runtime, name := runtimeRoleFixture(t, f.ctx, f.pool)
	ident := pgx.Identifier{name}.Sanitize()
	exec := func(t *testing.T, query string) {
		t.Helper()
		if _, err := f.pool.Exec(f.ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	// All startup checks must work with PostgreSQL enforcing read-only queries.
	exec(t, "ALTER ROLE "+ident+" SET default_transaction_read_only = on")
	runtime.Reset()
	check := func(t *testing.T, wantReady bool) {
		t.Helper()
		err := PrepareSchema(f.ctx, runtime, "../../migrations", "external", nil)
		if wantReady && err != nil {
			t.Fatalf("complete runtime privileges rejected: %v", err)
		}
		if !wantReady && (err == nil || !strings.Contains(err.Error(), "requires complete runtime privileges")) {
			t.Fatalf("expected missing runtime grants refusal, got %v", err)
		}
	}
	check(t, true)
	// Test each required bit independently: PostgreSQL's comma-separated
	// has_table_privilege argument means ANY, not ALL, of the supplied bits.
	for _, table := range runtimeTables {
		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			t.Run(table+"/"+privilege, func(t *testing.T) {
				qualified := pgx.Identifier{"public", table}.Sanitize()
				exec(t, "REVOKE "+privilege+" ON TABLE "+qualified+" FROM "+ident)
				check(t, false)
				var retained bool
				if err := f.pool.QueryRow(f.ctx, `SELECT has_table_privilege($1,$2,$3)`, name, qualified, privilege).Scan(&retained); err != nil || retained {
					t.Fatalf("startup repaired missing privilege: retained=%v err=%v", retained, err)
				}
				exec(t, "GRANT "+privilege+" ON TABLE "+qualified+" TO "+ident)
				check(t, true)
			})
		}
	}
	for _, privilege := range []string{"USAGE", "SELECT"} {
		exec(t, "REVOKE "+privilege+" ON SEQUENCE public.vpn_credential_revocation_events_sequence_seq FROM "+ident)
		check(t, false)
		exec(t, "GRANT "+privilege+" ON SEQUENCE public.vpn_credential_revocation_events_sequence_seq TO "+ident)
		check(t, true)
	}
	// A missing policy table must fail even with an intact migration ledger.
	exec(t, "ALTER TABLE public.audit_log RENAME TO rg140_hidden_audit_log")
	check(t, false)
	exec(t, "ALTER TABLE public.rg140_hidden_audit_log RENAME TO audit_log")
	check(t, true)
	// A present, complete ledger is insufficient after application ACL loss.
	exec(t, "REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM "+ident)
	exec(t, "GRANT SELECT ON public.schema_migrations TO "+ident)
	if err := CheckSchema(f.ctx, runtime, "../../migrations"); err != nil {
		t.Fatalf("version ledger should remain readable: %v", err)
	}
	check(t, false)
	// Administrative provisioning must still refuse to adopt an existing role.
	if err := ProvisionRuntimeRole(f.ctx, f.pool, name); err == nil {
		t.Fatal("existing login was silently repaired")
	}
	check(t, false)
}
