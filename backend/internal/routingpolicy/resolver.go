package routingpolicy

import (
	"net/netip"
	"sort"
	"strings"
)

const (
	SourceManual  = "manual"
	SourceManaged = "managed_rule_set"
)

type Matchers struct {
	Domains        []string
	DomainSuffixes []string
	DomainKeywords []string
	IPCIDRs        []string
}

type Candidate struct {
	ID       string
	Name     string
	Priority int
	Action   string
	Source   string
	Matchers Matchers
}

type Decision struct {
	Action    string
	Candidate *Candidate
}

// OrderCandidates is the single precedence definition shared by diagnostics and
// client rendering. Lower priority wins; an administrator rule wins a tie with
// a managed rule set; IDs provide a stable final tie-breaker.
func OrderCandidates(candidates []Candidate) []Candidate {
	ordered := append([]Candidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority < ordered[j].Priority
		}
		if ordered[i].Source != ordered[j].Source {
			return ordered[i].Source == SourceManual
		}
		return ordered[i].ID < ordered[j].ID
	})
	return ordered
}

func Resolve(target, defaultAction string, candidates []Candidate) Decision {
	for _, candidate := range OrderCandidates(candidates) {
		if Matches(target, candidate.Matchers) {
			matched := candidate
			return Decision{Action: candidate.Action, Candidate: &matched}
		}
	}
	return Decision{Action: defaultAction}
}

func Matches(target string, matchers Matchers) bool {
	target = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(target, ".")))
	if target == "" {
		return false
	}
	if addr, err := netip.ParseAddr(target); err == nil {
		for _, value := range matchers.IPCIDRs {
			if prefix, parseErr := netip.ParsePrefix(strings.TrimSpace(value)); parseErr == nil && prefix.Contains(addr) {
				return true
			}
		}
		return false
	}
	for _, value := range matchers.Domains {
		if target == normalizeDomain(value) {
			return true
		}
	}
	for _, value := range matchers.DomainSuffixes {
		suffix := normalizeDomain(value)
		if suffix != "" && (target == suffix || strings.HasSuffix(target, "."+suffix)) {
			return true
		}
	}
	for _, value := range matchers.DomainKeywords {
		if keyword := strings.ToLower(strings.TrimSpace(value)); keyword != "" && strings.Contains(target, keyword) {
			return true
		}
	}
	return false
}

func normalizeDomain(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(value, "."), ".")))
}
