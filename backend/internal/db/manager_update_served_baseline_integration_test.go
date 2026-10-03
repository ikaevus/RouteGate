package db

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Execute the runner's actual generated SQL, not a second Go implementation.
// Temporary tables isolate these deployment-model fixtures from the CI schema.
func TestManagerUpdateServedBaselineSQL(t *testing.T) {
	url := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	query, err := exec.CommandContext(ctx, "bash", "-c", `
source <(sed -n '/^check_block() {/,/^}/p; /^served_account_sql() {/,/^}/p' "$1")
served_account_sql "$2" Q5
`, "_", "../../../scripts/production-like-update-manager.sh", "../../../docs/operations/applied-client-settings/postflight-schema-158.sql").CombinedOutput()
	if err != nil {
		t.Fatalf("generate actual runner query: %v: %s", err, query)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	const node = "11111111-1111-4111-8111-111111111111"
	const served = "22222222-2222-4222-8222-222222222222"
	const pending = "33333333-3333-4333-8333-333333333333"
	_, err = tx.Exec(ctx, `
CREATE TEMP TABLE servers (id uuid, name text, active_config_version_id uuid) ON COMMIT DROP;
CREATE TEMP TABLE config_versions (id uuid, client_settings jsonb) ON COMMIT DROP;
CREATE TEMP TABLE vpn_accounts (id uuid, server_id uuid, status text) ON COMMIT DROP;
CREATE TEMP TABLE vpn_client_profiles (vpn_account_id uuid, active_protocol text) ON COMMIT DROP;
CREATE TEMP TABLE vpn_account_protocols (vpn_account_id uuid, protocol text, active_enabled boolean) ON COMMIT DROP;
INSERT INTO servers VALUES ('11111111-1111-4111-8111-111111111111', 'node', '44444444-4444-4444-8444-444444444444');
INSERT INTO config_versions VALUES ('44444444-4444-4444-8444-444444444444',
'{"vpnProtocol":"vless","accounts":{"22222222-2222-4222-8222-222222222222":{"primary":"vless","protocols":["vless"]}}}');
INSERT INTO vpn_accounts VALUES
('22222222-2222-4222-8222-222222222222', '11111111-1111-4111-8111-111111111111', 'active'),
('33333333-3333-4333-8333-333333333333', '11111111-1111-4111-8111-111111111111', 'active');
`)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		rows, err := tx.Query(ctx, string(query))
		if err != nil {
			t.Fatalf("execute runner SQL: %v", err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var serverID, accountID string
			if err := rows.Scan(&serverID, &accountID); err != nil {
				t.Fatal(err)
			}
			got = append(got, serverID+"|"+accountID)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, "\n") != want {
			t.Fatalf("served identities = %v, want %q", got, want)
		}
	}
	check(node + "|" + served) // New active-but-unapplied account is not baseline access.
	if _, err := tx.Exec(ctx, `UPDATE config_versions SET client_settings = jsonb_set(client_settings, '{accounts}',
'{"33333333-3333-4333-8333-333333333333":{"primary":"vless","protocols":["vless"]}}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	check(node + "|" + pending) // Same count, different identity: shell gate must reject loss.
	if _, err := tx.Exec(ctx, `UPDATE servers SET active_config_version_id = NULL`); err != nil {
		t.Fatal(err)
	}
	check("") // No first apply: no client access to protect yet.
}
