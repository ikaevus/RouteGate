package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func runtimeRoleFixture(t *testing.T, ctx context.Context, owner *pgxpool.Pool) (*pgxpool.Pool, string) {
	t.Helper()
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	name := "rg140_runtime_" + hex.EncodeToString(random[:8])
	password := hex.EncodeToString(random[8:])
	if err := ProvisionRuntimeRole(ctx, owner, name); err != nil {
		t.Fatal(err)
	}
	ident := pgx.Identifier{name}.Sanitize()
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := owner.Exec(cleanup, "DROP OWNED BY "+ident+"; DROP ROLE "+ident); err != nil {
			t.Errorf("remove isolated runtime role: %v", err)
		}
	})
	var elevated bool
	if err := owner.QueryRow(ctx, `SELECT rolcanlogin OR rolsuper OR rolcreatedb OR rolcreaterole
 OR rolinherit OR rolreplication OR rolbypassrls OR EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid)
 FROM pg_roles r WHERE rolname=$1`, name).Scan(&elevated); err != nil || elevated {
		t.Fatalf("unsafe provisioned role: elevated=%v err=%v", elevated, err)
	}
	// Login is enabled only for this disposable fixture, never by the policy.
	if _, err := owner.Exec(ctx, "ALTER ROLE "+ident+" LOGIN PASSWORD '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("ROUTEGATE_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.User, cfg.ConnConfig.Password = name, password
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	return pool, name
}

func TestRuntimeRolePolicy(t *testing.T) {
	f := newDispatchGateFixture(t)
	runtime, name := runtimeRoleFixture(t, f.ctx, f.pool)
	if err := CheckSchema(f.ctx, runtime, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err := auth.NewRepository(runtime).EnsureBuiltIns(f.ctx); err != nil {
		t.Fatalf("runtime bootstrap DML: %v", err)
	}
	var ownerRole string
	if err := f.pool.QueryRow(f.ctx, `SELECT current_user`).Scan(&ownerRole); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`CREATE TABLE public.rg140_forbidden(id int)`,
		`CREATE SCHEMA rg140_forbidden`,
		`DELETE FROM public.schema_migrations`,
		`TRUNCATE public.audit_log`,
		`ALTER TABLE public.vpn_credential_revocations DISABLE TRIGGER ALL`,
		`SET session_replication_role = replica`,
		"SET ROLE " + pgx.Identifier{ownerRole}.Sanitize(),
		"ALTER ROLE " + pgx.Identifier{name}.Sanitize() + " SUPERUSER",
		`SELECT setval('vpn_credential_revocation_events_sequence_seq', 1)`,
	} {
		_, err := runtime.Exec(f.ctx, query)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "42501" {
			t.Fatalf("expected 42501 for %q, got %v", query, err)
		}
	}
	// Unknown/future protected records must not gain ordinary DML rights.
	if _, err := f.pool.Exec(f.ctx, `CREATE TABLE public.rg140_future_authority(id int)`); err != nil {
		t.Fatal(err)
	}
	_, err := runtime.Exec(f.ctx, `INSERT INTO public.rg140_future_authority VALUES(1)`)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "42501" {
		t.Fatalf("future table acquired runtime writes: %v", err)
	}
	if err := ProvisionRuntimeRole(f.ctx, f.pool, name+"_next"); err == nil {
		t.Fatal("unknown public relation admitted by grant policy")
	}
	var exists bool
	if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, name+"_next").Scan(&exists); err != nil || exists {
		t.Fatalf("failed provisioning left a role: exists=%v err=%v", exists, err)
	}
	if _, err := f.pool.Exec(f.ctx, `DROP TABLE public.rg140_future_authority`); err != nil {
		t.Fatal(err)
	}
	if err := ProvisionRuntimeRole(f.ctx, f.pool, name); err == nil {
		t.Fatal("existing role was silently adopted")
	}
	// An unsafe PUBLIC default must fail atomically, not be silently rewritten.
	if _, err := f.pool.Exec(f.ctx, `GRANT CREATE ON SCHEMA public TO PUBLIC`); err != nil {
		t.Fatal(err)
	}
	if err := ProvisionRuntimeRole(f.ctx, f.pool, name+"_unsafe"); err == nil {
		t.Fatal("PUBLIC schema CREATE accepted")
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, name+"_unsafe").Scan(&exists); err != nil || exists {
		t.Fatalf("unsafe PUBLIC check left partial role: exists=%v err=%v", exists, err)
	}
	if _, err := f.pool.Exec(f.ctx, `REVOKE CREATE ON SCHEMA public FROM PUBLIC`); err != nil {
		t.Fatal(err)
	}
	for _, privilege := range []string{
		"TRUNCATE ON TABLE public.audit_log",
		"TRIGGER ON TABLE public.vpn_credential_revocations",
		"UPDATE ON SEQUENCE public.vpn_credential_revocation_events_sequence_seq",
	} {
		if _, err := f.pool.Exec(f.ctx, "GRANT "+privilege+" TO PUBLIC"); err != nil {
			t.Fatal(err)
		}
		if err := ProvisionRuntimeRole(f.ctx, f.pool, name+"_unsafe"); err == nil {
			t.Fatalf("unsafe PUBLIC privilege accepted: %s", privilege)
		}
		if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, name+"_unsafe").Scan(&exists); err != nil || exists {
			t.Fatalf("unsafe PUBLIC check left partial role: exists=%v err=%v", exists, err)
		}
		if _, err := f.pool.Exec(f.ctx, "REVOKE "+privilege+" FROM PUBLIC"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuntimeRoleGenericApplyFence(t *testing.T) {
	for _, staged := range []bool{false, true} {
		name := "approved"
		if staged {
			name = "staged"
		}
		t.Run(name, func(t *testing.T) {
			testCredentialRemovalGenericApplyFenceWithRole(t, staged, true)
		})
	}
}

func TestRuntimeRoleInvalidName(t *testing.T) {
	for _, name := range []string{"", "pg_shadow", "MixedCase", "bad;name", "runtime role"} {
		if err := ProvisionRuntimeRole(context.Background(), nil, name); err == nil {
			t.Fatalf("invalid role accepted: %q", name)
		}
	}
}
