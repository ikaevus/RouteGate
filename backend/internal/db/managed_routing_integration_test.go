package db

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/routingprofiles"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

type managedFixtureFetcher struct {
	document routingprofiles.SourceDocument
	err      error
}

func (f managedFixtureFetcher) Fetch(context.Context, string, string) (routingprofiles.SourceDocument, error) {
	return f.document, f.err
}
func TestManagedRoutingMigrationInheritanceRefreshAndDelivery(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := Connect(ctx, databaseURL, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	resetPublicSchema(t, ctx, pool)
	if err = Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatal(err)
	}
	repo := routingprofiles.NewRepository(pool)
	global, err := repo.GetEffectiveProfile(ctx, "", "")
	if err != nil || global.DefaultAction != "vpn" {
		t.Fatalf("global %+v %v", global, err)
	}
	serverProfile, err := repo.CreateProfile(ctx, routingprofiles.CreateRoutingProfileInput{Name: "Russia smart", DefaultAction: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	override, err := repo.CreateProfile(ctx, routingprofiles.CreateRoutingProfileInput{Name: "Account override", DefaultAction: "block"})
	if err != nil {
		t.Fatal(err)
	}
	var serverID string
	if err = pool.QueryRow(ctx, `INSERT INTO servers(name,public_ip) VALUES('routing-test','192.0.2.10') RETURNING id::text`).Scan(&serverID); err != nil {
		t.Fatal(err)
	}
	account, err := vpnaccounts.NewRepository(pool).CreateAccount(ctx, vpnaccounts.CreateAccountInput{DisplayName: "routing-test", ServerID: serverID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	effective, err := repo.GetEffectiveProfile(ctx, account.ID, serverID)
	if err != nil || effective.ID != global.ID {
		t.Fatalf("initial inheritance %+v %v", effective, err)
	}
	if _, err = repo.AssignServerProfile(ctx, routingprofiles.AssignServerRoutingProfileInput{ServerID: serverID, RoutingProfileID: serverProfile.ID}); err != nil {
		t.Fatal(err)
	}
	effective, err = repo.GetEffectiveProfile(ctx, account.ID, serverID)
	if err != nil || effective.ID != serverProfile.ID {
		t.Fatalf("server inheritance %+v %v", effective, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO vpn_account_routing_profiles(vpn_account_id,routing_profile_id) VALUES($1::uuid,$2::uuid)`, account.ID, override.ID); err != nil {
		t.Fatal(err)
	}
	effective, err = repo.GetEffectiveProfile(ctx, account.ID, serverID)
	if err != nil || effective.ID != override.ID {
		t.Fatalf("override inheritance %+v %v", effective, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM vpn_account_routing_profiles WHERE vpn_account_id=$1::uuid`, account.ID); err != nil {
		t.Fatal(err)
	}
	input := routingprofiles.ManagedSetInput{Name: "Blocked", Provider: "custom", SourceURL: "https://example.org/rules.json", Priority: 2000, Action: "vpn", Enabled: true, RefreshHours: 24}
	source, err := repo.CreateManagedSet(ctx, serverProfile.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = vpnaccounts.NewRepository(pool).GetSubscriptionProfileByAccountID(ctx, account.ID); err == nil {
		t.Fatal("delivered profile with enabled uninitialized source")
	}
	good := managedFixtureFetcher{document: routingprofiles.SourceDocument{Version: 1, Rules: []routingprofiles.DestinationRule{{DomainSuffixes: []string{"example.org"}}}}}
	source, err = repo.RefreshManagedSet(ctx, serverProfile.ID, source.ID, good)
	if err != nil || source.LastSuccessAt == nil || source.SnapshotSHA256 == "" {
		t.Fatalf("refresh %+v %v", source, err)
	}
	goodHash := source.SnapshotSHA256
	goodTime := *source.LastSuccessAt
	source, err = repo.RefreshManagedSet(ctx, serverProfile.ID, source.ID, managedFixtureFetcher{err: errors.New("invalid/empty source")})
	if err != nil || source.LastError == "" || source.SnapshotSHA256 != goodHash || !source.LastSuccessAt.Equal(goodTime) || source.Snapshot == nil {
		t.Fatalf("lost last good snapshot: %+v %v", source, err)
	}
	// A provider returning an empty document without an error must still fail safely.
	source, err = repo.RefreshManagedSet(ctx, serverProfile.ID, source.ID, managedFixtureFetcher{})
	if err != nil || source.LastError == "" || source.SnapshotSHA256 != goodHash {
		t.Fatalf("empty refresh replaced good state: %+v %v", source, err)
	}

	subscription, err := vpnaccounts.NewRepository(pool).GetSubscriptionProfileByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	config, err := vpnaccounts.RenderSingBoxClientConfig(subscription)
	if err != nil {
		t.Fatal(err)
	}
	if config.Route.Final != "direct" || len(config.Route.RuleSets) != 1 {
		t.Fatalf("client lost effective rules: %+v", config.Route)
	}
	d, err := subscription.RoutingProfile.Policy.Diagnose("example.org", "")
	if err != nil || d.Action != "vpn" || d.Winner.ID != source.ID {
		t.Fatalf("diagnostic mismatch %+v %v", d, err)
	}
	_, err = repo.CreateRule(ctx, routingprofiles.CreateRoutingProfileRuleInput{RoutingProfileID: serverProfile.ID, Name: "Manual direct", Priority: 100, Action: "direct", Domains: []string{"example.org"}, DomainSuffixes: []string{}, DomainKeywords: []string{}, IPCIDRs: []string{}, GeoSites: []string{}, GeoIPs: []string{}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	subscription, err = vpnaccounts.NewRepository(pool).GetSubscriptionProfileByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	config, err = vpnaccounts.RenderSingBoxClientConfig(subscription)
	if err != nil || config.Route.Rules[0]["outbound"] != "direct" {
		t.Fatalf("manual renderer override %+v %v", config.Route, err)
	}
	d, err = subscription.RoutingProfile.Policy.Diagnose("example.org", "")
	if err != nil || d.Action != "direct" || d.Winner.Kind != "manual" {
		t.Fatalf("manual diagnostic %+v %v", d, err)
	}
	input.Enabled = false
	input.Name = "Paused source"
	if _, err = repo.UpdateManagedSet(ctx, serverProfile.ID, source.ID, input); err != nil {
		t.Fatal(err)
	}
	subscription, err = vpnaccounts.NewRepository(pool).GetSubscriptionProfileByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	config, err = vpnaccounts.RenderSingBoxClientConfig(subscription)
	if err != nil || len(config.Route.RuleSets) != 0 {
		t.Fatalf("disabled list still rendered %+v %v", config.Route, err)
	}
	if err = repo.DeleteManagedSet(ctx, serverProfile.ID, source.ID); err != nil {
		t.Fatal(err)
	}
	action := "block"
	name := "Changed default"
	changed, err := repo.UpdateProfile(ctx, serverProfile.ID, routingprofiles.UpdateRoutingProfileInput{DefaultAction: &action, Name: &name})
	if err != nil || changed.DefaultAction != "block" {
		t.Fatalf("default action update %+v %v", changed, err)
	}
	makeDefault := true
	changed, err = repo.UpdateProfile(ctx, serverProfile.ID, routingprofiles.UpdateRoutingProfileInput{DefaultAction: &action, IsDefault: &makeDefault})
	if err != nil || !changed.IsDefault || changed.DefaultAction != "block" {
		t.Fatalf("global default update %+v %v", changed, err)
	}
	// Roll back only this feature migration, leaving historical tables/accounts intact.
	down, err := os.ReadFile("../../migrations/000146_managed_routing_rule_sets.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM vpn_accounts WHERE id=$1::uuid`, account.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rollback damaged account: %d %v", count, err)
	}
}
