package routingprofiles

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func testPolicy(t *testing.T, manual []RoutingProfileRule, sets []ManagedRuleSet) Policy {
	t.Helper()
	p, err := CompilePolicy(RoutingProfile{ID: "p", Name: "Russia", DefaultAction: ActionDirect, Rules: manual, ManagedSets: sets})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func testSet(id string, priority int) ManagedRuleSet {
	return ManagedRuleSet{ID: id, Name: "RU Blocked", Provider: "custom", Enabled: true, Priority: priority, Action: ActionVPN, Snapshot: &SourceDocument{Version: 1, Rules: []DestinationRule{{DomainSuffixes: []string{"example.org"}, IPCIDRs: []string{"203.0.113.0/24"}}}}}
}
func TestManagedPolicyManualOverridesAndRenderer(t *testing.T) {
	for _, action := range []string{ActionDirect, ActionVPN, ActionBlock} {
		t.Run(action, func(t *testing.T) {
			p := testPolicy(t, []RoutingProfileRule{{ID: "override", Name: "Administrator override", Enabled: true, Priority: 100, Action: action, Domains: []string{"example.org"}}}, []ManagedRuleSet{testSet("s", 2000)})
			d, err := p.Diagnose("EXAMPLE.ORG.", "")
			if err != nil || d.Action != action || d.Winner.ID != "override" || d.Order != 1 {
				t.Fatalf("diagnostic: %+v %v", d, err)
			}
			rules, sets, final := p.Render("vpn-current-node")
			if final != "direct" || len(sets) != 1 || len(rules) != 2 {
				t.Fatalf("render %+v %+v %s", rules, sets, final)
			}
			if action == ActionBlock {
				if rules[0]["action"] != "reject" {
					t.Fatal(rules)
				}
			} else if rules[0]["outbound"] != outboundFor(action, "vpn-current-node") {
				t.Fatal(rules)
			}
			if rules[1]["outbound"] != "vpn-current-node" {
				t.Fatal(rules)
			}
		})
	}
}
func TestManagedPolicyPrecedenceAndDefaults(t *testing.T) {
	early := time.Unix(1, 0)
	late := time.Unix(2, 0)
	a := testSet("a", 10)
	a.CreatedAt = early
	b := testSet("b", 10)
	b.CreatedAt = early
	p := testPolicy(t, []RoutingProfileRule{{ID: "0", Name: "later", Enabled: true, Priority: 10, Action: ActionBlock, Domains: []string{"example.org"}, CreatedAt: late}}, []ManagedRuleSet{b, a})
	if p.Entries[0].ID != "a" || p.Entries[1].ID != "b" || p.Entries[2].ID != "0" {
		t.Fatal(p.Entries)
	}
	d, _ := p.Diagnose("example.org", "")
	if d.Action != ActionVPN || d.Winner.ID != "a" {
		t.Fatal(d)
	}
	d, _ = p.Diagnose("203.0.113.7", "")
	if d.Action != ActionVPN {
		t.Fatal(d)
	}
	// No client DNS data: do not claim an unrelated domain certainly routes direct.
	d, _ = p.Diagnose("native.ru", "")
	if d.Status != "indeterminate" || d.Action != "" {
		t.Fatal(d)
	}
	d, _ = p.Diagnose("native.ru", "192.0.2.1")
	if d.Action != ActionDirect || d.Status != "default" {
		t.Fatal(d)
	}
	p, err := CompilePolicy(RoutingProfile{})
	if err != nil || p.DefaultAction != ActionVPN {
		t.Fatalf("legacy default %+v %v", p, err)
	}
	for _, action := range []string{ActionDirect, ActionVPN, ActionBlock} {
		p.DefaultAction = action
		d, _ = p.Diagnose("native.ru", "")
		if d.Action != action {
			t.Fatal(d)
		}
	}
	p.DefaultAction = ActionBlock
	r, _, _ := p.Render("vpn")
	if len(r) != 1 || r[0]["action"] != "reject" {
		t.Fatal(r)
	}
}
func TestManagedPolicyDisabledAndMissingSnapshot(t *testing.T) {
	s := testSet("s", 1)
	s.Snapshot = nil
	if _, err := CompilePolicy(RoutingProfile{ManagedSets: []ManagedRuleSet{s}}); err == nil {
		t.Fatal("missing snapshot silently accepted")
	}
	s.Enabled = false
	p := testPolicy(t, []RoutingProfileRule{{Enabled: false, Action: ActionBlock, Domains: []string{"x.org"}}}, []ManagedRuleSet{s})
	if len(p.Entries) != 0 {
		t.Fatal(p)
	}
}
func TestPolicyDomainBoundaryRegexAndIP(t *testing.T) {
	cases := []struct {
		r          DestinationRule
		domain, ip string
		want       bool
	}{
		{DestinationRule{DomainSuffixes: []string{"example.org"}}, "example.org", "", true},
		{DestinationRule{DomainSuffixes: []string{"example.org"}}, "sub.example.org", "", true},
		{DestinationRule{DomainSuffixes: []string{"example.org"}}, "notexample.org", "", false},
		{DestinationRule{DomainSuffixes: []string{".example.org"}}, "example.org", "", false},
		{DestinationRule{DomainSuffixes: []string{".example.org"}}, "sub.example.org", "", true},
		{DestinationRule{DomainRegex: []string{`^video[0-9]+\.example\.org$`}}, "video12.example.org", "", true},
		{DestinationRule{DomainKeywords: []string{"example"}}, "example.net", "", true},
	}
	for _, c := range cases {
		s := testSet("s", 1)
		s.Snapshot.Rules = []DestinationRule{c.r}
		p := testPolicy(t, nil, []ManagedRuleSet{s})
		d, err := p.Diagnose(c.domain, c.ip)
		if err != nil || (d.Action == ActionVPN) != c.want {
			t.Fatalf("%+v: %+v %v", c, d, err)
		}
	}
	p := testPolicy(t, []RoutingProfileRule{{ID: "legacy", Enabled: true, Action: ActionVPN, Priority: 1, GeoSites: []string{"ru"}}}, nil)
	d, _ := p.Diagnose("example.org", "")
	if d.Status != "indeterminate" {
		t.Fatal(d)
	}
	for _, input := range []string{"https://example.org/path", "a b.org", "", "1.2.3.4/24"} {
		if _, err := p.Diagnose(input, ""); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}
func TestSourceValidationAndAdapters(t *testing.T) {
	valid := `{"version":1,"rules":[{"domain":["EXAMPLE.ORG"],"ip_cidr":["203.0.113.1","2001:db8::/32"]}]}`
	doc, err := ParseSource([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Rules[0].Domains[0] != "example.org" || doc.Rules[0].IPCIDRs[0] != "203.0.113.1/32" {
		t.Fatal(doc)
	}
	for _, v := range []string{`{}`, `{"version":1,"rules":[]}`, `{"version":1,"rules":[{}]}`, `{"version":4,"rules":[{"domain":["x.org"]}]}`, `{"version":1,"rules":[{"domain":["x.org"],"port":[443]}]}`, `{"version":1,"rules":[{"domain_regex":["["]}]}`, `{"version":1,"rules":[{"ip_cidr":["bad"]}]}`, valid + `{}`} {
		if _, err := ParseSource([]byte(v)); err == nil {
			t.Fatalf("accepted %s", v)
		}
	}
	doc, err = parseDomainSource([]byte("# comment\ndomain:example.org\nfull:exact.example.org\nkeyword:video\nregexp:^test[0-9]+\\.org$\nplain.org.\n"))
	if err != nil || len(doc.Rules[0].DomainSuffixes) != 2 {
		t.Fatalf("text %+v %v", doc, err)
	}
	if _, err := parseDomainSource([]byte("include:other")); err == nil {
		t.Fatal("silently ignored include")
	}
}

// Optional real-source validation stays offline by default. CI need not trust
// availability or content of third-party endpoints.
func TestDownloadedProviderSnapshots(t *testing.T) {
	for _, key := range []string{"ROUTEGATE_TEST_REFILTER_SOURCE", "ROUTEGATE_TEST_RUNETFREEDOM_SOURCE"} {
		t.Run(key, func(t *testing.T) {
			path := os.Getenv(key)
			if path == "" {
				t.Skip("download fixture path not set")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := parseDomainSource(data)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(doc)
			if len(raw) < 100 {
				t.Fatal("unexpected tiny snapshot")
			}
			t.Logf("validated %d source bytes -> %d snapshot bytes", len(data), len(raw))
		})
	}
}
func TestSourceURLPolicy(t *testing.T) {
	for _, u := range []string{"http://example.org/x", "https://user:pass@example.org/x", "https://127.0.0.1/x", "https://[::1]/x", "https://169.254.169.254/x", "https://10.0.0.1/x", "https://example.org:8443/x", "https://example.org/x#part"} {
		if err := validateSourceURL(u); err == nil {
			t.Fatal(u)
		}
	}
	if err := validateSourceURL("https://example.org/list.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSource([]byte(strings.Repeat(" ", maxSourceBytes+1))); err == nil {
		t.Fatal("oversized source accepted")
	}
}
