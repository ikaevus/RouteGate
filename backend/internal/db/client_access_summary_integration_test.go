package db

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/configs"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

// The Dashboard asks whether a node issues client access to at least one
// active account. The answer must come from the evaluation that issues client
// links, cover every active account (not only the newest), leave no profile or
// protocol rows behind, and report an unfinished evaluation as unknown.
// Version numbers belong to the isolated test database only.
func TestClientAccessSummaryEvaluatesEveryActiveAccountWithoutWriting(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	accounts := vpnaccounts.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	handler := newSubscriptionProbe(t, ctx, pool, accounts).handler

	// served-old: an old account served by the applied configuration and three
	// newer accounts created after that apply.
	oldNode := createSummaryServer(t, ctx, pool, "summary-old-served")
	oldAccount := createSummaryAccount(t, ctx, pool, oldNode, "2026-01-01")
	readAccount(t, ctx, accounts, oldAccount)
	v1 := renderVersion(t, ctx, render, oldNode)
	finishApply(t, ctx, pool, oldNode, v1, "succeeded")
	newer := []string{
		createSummaryAccount(t, ctx, pool, oldNode, "2026-02-01"),
		createSummaryAccount(t, ctx, pool, oldNode, "2026-03-01"),
		createSummaryAccount(t, ctx, pool, oldNode, "2026-04-01"),
	}

	// newest-only: three older accounts in the applied configuration whose
	// access is withheld (their primary drifted from what the node deploys),
	// and only the newest one served.
	newestNode := createSummaryServer(t, ctx, pool, "summary-newest-served")
	withheld := []string{
		createSummaryAccount(t, ctx, pool, newestNode, "2026-01-01"),
		createSummaryAccount(t, ctx, pool, newestNode, "2026-01-02"),
		createSummaryAccount(t, ctx, pool, newestNode, "2026-01-03"),
	}
	newestAccount := createSummaryAccount(t, ctx, pool, newestNode, "2026-01-04")
	readAccount(t, ctx, accounts, append(append([]string{}, withheld...), newestAccount)...)
	v2 := renderVersion(t, ctx, render, newestNode)
	finishApply(t, ctx, pool, newestNode, v2, "succeeded")
	for _, id := range withheld {
		if _, err := pool.Exec(ctx, `UPDATE vpn_client_profiles SET active_protocol = 'wireguard' WHERE vpn_account_id = $1::uuid`, id); err != nil {
			t.Fatalf("drift primary: %v", err)
		}
	}

	// never-applied: accounts, no successful apply.
	neverNode := createSummaryServer(t, ctx, pool, "summary-never-applied")
	neverAccount := createSummaryAccount(t, ctx, pool, neverNode, "2026-01-01")
	readAccount(t, ctx, accounts, neverAccount)
	renderVersion(t, ctx, render, neverNode)

	// mtproto-only: served through the node-wide proxy without an
	// active_enabled row for the account.
	mtprotoNode := createSummaryServer(t, ctx, pool, "summary-mtproto-only")
	if _, err := pool.Exec(ctx, `UPDATE servers SET vpn_protocol = 'mtproto', mtproto_port = 9443 WHERE id = $1::uuid`, mtprotoNode); err != nil {
		t.Fatalf("switch node to MTProto only: %v", err)
	}
	mtprotoAccount := createSummaryAccount(t, ctx, pool, mtprotoNode, "2026-01-01")
	v3 := renderVersion(t, ctx, render, mtprotoNode)
	finishApply(t, ctx, pool, mtprotoNode, v3, "succeeded")

	// empty: an applied node whose only account is no longer active.
	emptyNode := createSummaryServer(t, ctx, pool, "summary-empty")
	formerAccount := createSummaryAccount(t, ctx, pool, emptyNode, "2026-01-01")
	readAccount(t, ctx, accounts, formerAccount)
	v4 := renderVersion(t, ctx, render, emptyNode)
	finishApply(t, ctx, pool, emptyNode, v4, "succeeded")
	if _, err := pool.Exec(ctx, `UPDATE vpn_accounts SET status = 'suspended' WHERE id = $1::uuid`, formerAccount); err != nil {
		t.Fatalf("suspend account: %v", err)
	}

	before := summaryFootprint(t, ctx, pool)
	summary := readClientAccessSummary(t, handler)
	if after := summaryFootprint(t, ctx, pool); after != before {
		t.Fatalf("the summary must not write profiles or protocol rows: before=%+v after=%+v", before, after)
	}

	assertNodeAccess(t, summary, oldNode, vpnaccounts.NodeClientAccessServed, 4)
	if got := summary[oldNode].ServedAccountID; got != oldAccount {
		t.Fatalf("old served account not found behind three newer pending accounts: served=%q want %q", got, oldAccount)
	}
	assertNodeAccess(t, summary, newestNode, vpnaccounts.NodeClientAccessServed, 4)
	if got := summary[newestNode].ServedAccountID; got != newestAccount {
		t.Fatalf("newest served account not found behind three withheld accounts: served=%q want %q", got, newestAccount)
	}
	assertNodeAccess(t, summary, neverNode, vpnaccounts.NodeClientAccessNotServed, 1)
	if got := summary[neverNode]; got.PendingAccountID != neverAccount || got.PendingStatus != vpnaccounts.ClientConnectionStatusAwaitingFirstApply {
		t.Fatalf("never-applied node: %+v", got)
	}
	assertNodeAccess(t, summary, mtprotoNode, vpnaccounts.NodeClientAccessServed, 1)
	if _, listed := summary[emptyNode]; listed {
		t.Fatalf("a node without active accounts has nothing to evaluate: %+v", summary[emptyNode])
	}

	// The summary agrees with GET /client-profile, account by account.
	for node, ids := range map[string][]string{
		oldNode: append([]string{oldAccount}, newer...), newestNode: append(append([]string{}, withheld...), newestAccount),
		neverNode: {neverAccount}, mtprotoNode: {mtprotoAccount},
	} {
		served := false
		for _, id := range ids {
			state := readClientProfileState(t, handler, id)
			served = served || state.ConnectionStatus == vpnaccounts.ClientConnectionStatusReady
		}
		if want := summary[node].State == vpnaccounts.NodeClientAccessServed; served != want {
			t.Fatalf("node %s: client-profile served=%v, summary state=%s", node, served, summary[node].State)
		}
	}
	for _, id := range withheld {
		if state := readClientProfileState(t, handler, id); state.ConnectionStatus == vpnaccounts.ClientConnectionStatusReady {
			t.Fatalf("a drifted older account must not be served: %s", state.ConnectionStatus)
		}
	}
	for _, id := range newer {
		if state := readClientProfileState(t, handler, id); state.ConnectionStatus != vpnaccounts.ClientConnectionStatusAwaitingApply {
			t.Fatalf("an account created after the apply must await it: %s", state.ConnectionStatus)
		}
	}

	// An evaluation that cannot finish is unknown, never "not served".
	lockedNode := createSummaryServer(t, ctx, pool, "summary-locked")
	lockedAccount := createSummaryAccount(t, ctx, pool, lockedNode, "2026-01-01")
	readAccount(t, ctx, accounts, lockedAccount)
	v5 := renderVersion(t, ctx, render, lockedNode)
	finishApply(t, ctx, pool, lockedNode, v5, "succeeded")
	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Exec(ctx, `SELECT 1 FROM vpn_client_profiles WHERE vpn_account_id = $1::uuid FOR UPDATE`, lockedAccount); err != nil {
		t.Fatalf("lock profile: %v", err)
	}
	summary = readClientAccessSummary(t, handler)
	_ = lock.Rollback(ctx)
	assertNodeAccess(t, summary, lockedNode, vpnaccounts.NodeClientAccessUnknown, 1)
	assertNodeAccess(t, summary, oldNode, vpnaccounts.NodeClientAccessServed, 4)
}

func createSummaryServer(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) string {
	t.Helper()
	key := newRealityTestKey(t)
	var serverID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (
			name, status, deployment_role, public_ip, vpn_protocol, vless_port, vless_flow, vless_network,
			reality_private_key, reality_public_key, reality_short_id, reality_server_name
		) VALUES ($1, 'active', 'vpn', '203.0.113.10', 'vless', 8443, 'xtls-rprx-vision', 'tcp',
			$2, $3, '0123456789abcdef', 'www.microsoft.com')
		RETURNING id::text
	`, name, key.private, key.public).Scan(&serverID); err != nil {
		t.Fatalf("create server %s: %v", name, err)
	}
	return serverID
}

func createSummaryAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, serverID, created string) string {
	t.Helper()
	var accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status, server_id, vless_uuid, created_at)
		VALUES ('acct-' || substr(md5(random()::text), 1, 8), 'sing-box', 'Summary', 'active', $1::uuid, gen_random_uuid(), $2::timestamptz)
		RETURNING id::text
	`, serverID, created).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return accountID
}

// readAccount mimics an administrator opening the accounts before an apply.
func readAccount(t *testing.T, ctx context.Context, accounts *vpnaccounts.Repository, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := accounts.GetOrCreateClientProfile(ctx, id); err != nil {
			t.Fatalf("profile: %v", err)
		}
		if _, _, err := accounts.GetClientProtocolSets(ctx, id); err != nil {
			t.Fatalf("protocol sets: %v", err)
		}
	}
}

type footprint struct{ profiles, protocolRows, servers string }

func summaryFootprint(t *testing.T, ctx context.Context, pool *pgxpool.Pool) footprint {
	t.Helper()
	var value footprint
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT COALESCE(md5(string_agg(row(p.*)::text, ',' ORDER BY p.id)), '') FROM vpn_client_profiles p),
			(SELECT COALESCE(md5(string_agg(row(r.*)::text, ',' ORDER BY r.vpn_account_id, r.protocol)), '') FROM vpn_account_protocols r),
			(SELECT COALESCE(md5(string_agg(row(s.*)::text, ',' ORDER BY s.id)), '') FROM servers s)
	`).Scan(&value.profiles, &value.protocolRows, &value.servers); err != nil {
		t.Fatalf("read footprint: %v", err)
	}
	return value
}

func readClientAccessSummary(t *testing.T, handler *vpnaccounts.Handler) map[string]vpnaccounts.NodeClientAccess {
	t.Helper()
	response := httptest.NewRecorder()
	handler.GetClientAccessSummary(response, httptest.NewRequest(http.MethodGet, "/api/v1/vpn-accounts/access-summary", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("access summary: status=%d body=%s", response.Code, response.Body.String())
	}
	var decoded vpnaccounts.ClientAccessSummaryResponse
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode access summary: %v", err)
	}
	items := map[string]vpnaccounts.NodeClientAccess{}
	for _, item := range decoded.Items {
		items[item.ServerID] = item
	}
	return items
}

func assertNodeAccess(t *testing.T, summary map[string]vpnaccounts.NodeClientAccess, serverID, state string, active int) {
	t.Helper()
	got, ok := summary[serverID]
	if !ok || got.State != state || got.ActiveAccounts != active {
		t.Fatalf("node %s: got %+v (listed=%v), want state=%s activeAccounts=%d", serverID, got, ok, state, active)
	}
}
