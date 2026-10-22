package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This is a real, separately authenticated non-owner connection, not SET ROLE
// on a privileged session. The only table privilege is SELECT on the ledger.
func TestSchemaReadinessWithReadOnlyLogin(t *testing.T) {
	url := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	owner, err := Connect(ctx, url, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	resetPublicSchema(t, ctx, owner)
	if err := Migrate(ctx, owner, "../../migrations", logger); err != nil {
		t.Fatal(err)
	}
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	role := "rg140_schema_" + hex.EncodeToString(random[:8])
	password := hex.EncodeToString(random[8:])
	ident := pgx.Identifier{role}.Sanitize()
	if _, err := owner.Exec(ctx, "CREATE ROLE "+ident+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS PASSWORD '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		// Only this unique fixture role is affected. It owns no objects.
		if _, err := owner.Exec(cleanup, "DROP OWNED BY "+ident+"; DROP ROLE "+ident); err != nil {
			t.Errorf("remove fixture role: %v", err)
		}
	}()
	var database, ownerRole string
	if err := owner.QueryRow(ctx, `SELECT current_database(), current_user`).Scan(&database, &ownerRole); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, "GRANT CONNECT ON DATABASE "+pgx.Identifier{database}.Sanitize()+" TO "+ident+"; GRANT USAGE ON SCHEMA public TO "+ident+"; GRANT SELECT ON public.schema_migrations TO "+ident); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.User = role
	cfg.ConnConfig.Password = password
	runtime, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if err := runtime.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := PrepareSchema(ctx, runtime, "../../migrations", "external", logger); err != nil {
		t.Fatalf("read-only readiness: %v", err)
	}
	for _, query := range []string{
		`CREATE TABLE public.rg140_forbidden(id int)`,
		`DELETE FROM public.schema_migrations`,
		`ALTER TABLE public.vpn_credential_revocations DISABLE TRIGGER ALL`,
		"SET ROLE " + pgx.Identifier{ownerRole}.Sanitize(),
	} {
		_, err := runtime.Exec(ctx, query)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "42501" {
			t.Fatalf("expected permission denial for %q, got %v", query, err)
		}
	}
	// A hole below the maximum must fail, and the check must not repair it.
	var removed string
	if err := owner.QueryRow(ctx, `DELETE FROM schema_migrations WHERE version=(SELECT min(version) FROM schema_migrations) RETURNING version`).Scan(&removed); err != nil {
		t.Fatal(err)
	}
	if err := CheckSchema(ctx, runtime, "../../migrations"); err == nil {
		t.Fatal("missing historical migration accepted")
	}
	var exists bool
	if err := owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, removed).Scan(&exists); err != nil || exists {
		t.Fatalf("readiness modified history: exists=%v err=%v", exists, err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, removed); err != nil {
		t.Fatal(err)
	}
	// Simulate an older binary's manifest without changing the real schema.
	older := t.TempDir()
	if err := os.WriteFile(filepath.Join(older, removed+".up.sql"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckSchema(ctx, runtime, older); err == nil {
		t.Fatal("newer database accepted by older manifest")
	}
	if _, err := owner.Exec(ctx, `ALTER TABLE schema_migrations RENAME TO rg140_hidden_migration_history`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := owner.Exec(context.Background(), `ALTER TABLE rg140_hidden_migration_history RENAME TO schema_migrations`); err != nil {
			t.Errorf("restore history name: %v", err)
		}
	}()
	if err := CheckSchema(ctx, runtime, "../../migrations"); err == nil {
		t.Fatal("absent history accepted")
	}
}
