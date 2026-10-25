package db

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PrepareSchema keeps legacy startup explicit while allowing a separately
// provisioned runtime connection to check, never migrate, its schema. Unknown
// modes must not fall back to the privileged path.
func PrepareSchema(ctx context.Context, pool *pgxpool.Pool, dir, mode string, logger *slog.Logger) error {
	switch mode {
	case "", "auto": // Empty preserves existing programmatic Config callers.
		return Migrate(ctx, pool, dir, logger)
	case "external":
		if err := CheckRuntimeOwnership(ctx, pool); err != nil {
			return err
		}
		return CheckSchema(ctx, pool, dir)
	default:
		return fmt.Errorf("invalid database migration mode: expected auto or external")
	}
}

// CheckSchema performs only SELECT. Compare the complete migration manifest,
// not its maximum: gaps and newer databases must both block startup. This is
// version readiness, not verification of migration contents or DB privileges.
func CheckSchema(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil || len(files) == 0 {
		return fmt.Errorf("schema readiness: migration manifest unavailable or empty")
	}
	expected := make(map[string]bool, len(files))
	for _, file := range files {
		expected[strings.TrimSuffix(filepath.Base(file), ".up.sql")] = true
	}
	rows, err := pool.Query(ctx, `SELECT version FROM public.schema_migrations`)
	if err != nil {
		return fmt.Errorf("schema readiness: read migration history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return fmt.Errorf("schema readiness: decode migration history: %w", err)
		}
		if !expected[version] {
			return fmt.Errorf("schema readiness: database has an unexpected migration")
		}
		delete(expected, version)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("schema readiness: read migration history: %w", err)
	}
	if len(expected) != 0 {
		return fmt.Errorf("schema readiness: database is missing required migrations")
	}
	return nil
}

// CheckRuntimeOwnership refuses privileged serving credentials in external mode.
// Membership is checked even for NOINHERIT roles: SET ROLE must not recover
// owner authority. This is a startup prerequisite, not a complete ACL audit.
func CheckRuntimeOwnership(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe bool
	err := pool.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM pg_catalog.pg_roles r
 WHERE pg_catalog.pg_has_role(session_user, r.oid, 'MEMBER')
 AND (r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls
 OR r.oid IN (SELECT datdba FROM pg_catalog.pg_database WHERE datname=current_database())
 OR r.oid IN (SELECT nspowner FROM pg_catalog.pg_namespace WHERE nspname='public')
 OR r.oid IN (SELECT c.relowner FROM pg_catalog.pg_class c
 JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'))
 )`).Scan(&unsafe)
	if err != nil {
		return fmt.Errorf("runtime ownership check: %w", err)
	}
	if unsafe {
		return fmt.Errorf("external migration mode refuses privileged runtime credentials")
	}
	return nil
}
