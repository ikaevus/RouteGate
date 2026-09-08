package routingpolicy

import "testing"

func TestResolveManualWinsManagedPriorityTie(t *testing.T) {
	candidates := []Candidate{
		{ID: "managed", Name: "Blocked", Priority: 100, Action: "vpn", Source: SourceManaged, Matchers: Matchers{DomainSuffixes: []string{"example.org"}}},
		{ID: "manual", Name: "Admin override", Priority: 100, Action: "direct", Source: SourceManual, Matchers: Matchers{Domains: []string{"video.example.org"}}},
	}
	decision := Resolve("video.example.org", "direct", candidates)
	if decision.Action != "direct" || decision.Candidate == nil || decision.Candidate.ID != "manual" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestResolveDeterministicPriorityAndMatchers(t *testing.T) {
	candidates := []Candidate{
		{ID: "later", Priority: 200, Action: "block", Source: SourceManual, Matchers: Matchers{DomainKeywords: []string{"example"}}},
		{ID: "first", Priority: 100, Action: "vpn", Source: SourceManaged, Matchers: Matchers{DomainSuffixes: []string{"example.org"}, IPCIDRs: []string{"203.0.113.0/24"}}},
	}
	for _, target := range []string{"www.example.org", "203.0.113.8"} {
		decision := Resolve(target, "direct", candidates)
		if decision.Action != "vpn" || decision.Candidate == nil || decision.Candidate.ID != "first" {
			t.Fatalf("target %q: unexpected decision: %+v", target, decision)
		}
	}
	if decision := Resolve("native.test", "direct", candidates); decision.Action != "direct" || decision.Candidate != nil {
		t.Fatalf("unexpected default decision: %+v", decision)
	}
}
