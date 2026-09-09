package vpnaccounts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/routingprofiles"
)

func TestManagedRoutingClientConfigAndSingBoxCheck(t *testing.T) {
	for _, defaultAction := range []string{"direct", "vpn", "block"} {
		t.Run(defaultAction, func(t *testing.T) {
			policy, err := routingprofiles.CompilePolicy(routingprofiles.RoutingProfile{ID: "p", DefaultAction: defaultAction,
				Rules:       []routingprofiles.RoutingProfileRule{{ID: "manual", Enabled: true, Action: "block", Priority: 10, Domains: []string{"malware.example"}}},
				ManagedSets: []routingprofiles.ManagedRuleSet{{ID: "managed", Enabled: true, Action: "vpn", Priority: 2000, Snapshot: &routingprofiles.SourceDocument{Version: 1, Rules: []routingprofiles.DestinationRule{{DomainSuffixes: []string{"example.org"}, IPCIDRs: []string{"203.0.113.0/24"}}}}}}})
			if err != nil {
				t.Fatal(err)
			}
			config, err := RenderSingBoxClientConfig(SubscriptionProfile{Account: Account{VLESSUUID: testVLESSUUID}, Server: &SubscriptionServer{PublicIP: "192.0.2.10", VLESSPort: 443}, RoutingProfile: &RoutingProfile{Policy: &policy}})
			if err != nil {
				t.Fatal(err)
			}
			if len(config.Route.RuleSets) != 1 || config.Route.Rules[0]["action"] != "reject" || config.Route.Rules[1]["outbound"] != singBoxOutboundTag {
				t.Fatalf("routing not applied %+v", config.Route)
			}
			if defaultAction == "direct" && config.Route.Final != "direct" {
				t.Fatal(config.Route)
			}
			if len(config.Outbounds) != 2 || config.Outbounds[1].Type != "direct" {
				t.Fatal(config.Outbounds)
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			binary := os.Getenv("ROUTEGATE_TEST_SING_BOX")
			if binary == "" {
				t.Log("sing-box executable check not configured")
				return
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(binary, "check", "-c", path).CombinedOutput(); err != nil {
				t.Fatalf("sing-box rejected config: %v\n%s", err, output)
			}
		})
	}
}
