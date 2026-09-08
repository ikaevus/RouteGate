package routingprofiles

import (
	"encoding/json"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/routingpolicy"
)

func TestDiagnosticCandidatesMatchRendererPrecedence(t *testing.T) {
	profile := RoutingProfile{DefaultAction: ActionDirect, Rules: []RoutingProfileRule{
		{ID: "override", Name: "Administrator override", Priority: 100, Action: ActionBlock, Domains: []string{"example.org"}, Enabled: true},
	}}
	snapshot, _ := json.Marshal(ManagedRuleSetSnapshot{Version: 1, Rules: []ManagedRuleSetMatcher{{DomainSuffixes: []string{"example.org"}}}})
	managed := []managedRuleSetRecord{{ManagedRuleSet: ManagedRuleSet{ID: "managed", Name: "RU blocked", Priority: 100, Action: ActionVPN, Enabled: true}, Snapshot: snapshot}}
	decision := routingpolicy.Resolve("example.org", profile.DefaultAction, diagnosticCandidates(profile, managed))
	if decision.Action != ActionBlock || decision.Candidate == nil || decision.Candidate.ID != "override" {
		t.Fatalf("unexpected diagnostic decision: %+v", decision)
	}
}

func TestDiagnosticCandidatesSupportManagedRuleSetAndDefault(t *testing.T) {
	profile := RoutingProfile{DefaultAction: ActionDirect}
	snapshot, _ := json.Marshal(ManagedRuleSetSnapshot{Version: 1, Rules: []ManagedRuleSetMatcher{{IPCIDRs: []string{"203.0.113.0/24"}}}})
	managed := []managedRuleSetRecord{{ManagedRuleSet: ManagedRuleSet{ID: "managed", Name: "Blocked", Priority: 200, Action: ActionVPN, Enabled: true}, Snapshot: snapshot}}
	candidates := diagnosticCandidates(profile, managed)
	if decision := routingpolicy.Resolve("203.0.113.8", profile.DefaultAction, candidates); decision.Action != ActionVPN || decision.Candidate == nil {
		t.Fatalf("unexpected managed decision: %+v", decision)
	}
	if decision := routingpolicy.Resolve("native.example", profile.DefaultAction, candidates); decision.Action != ActionDirect || decision.Candidate != nil {
		t.Fatalf("unexpected default decision: %+v", decision)
	}
}
