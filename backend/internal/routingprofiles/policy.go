package routingprofiles

import (
	"errors"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Policy is the ordered intermediate representation shared by client rendering
// and diagnostics. Lower priority, earlier creation, then UUID wins, across both kinds.
type Policy struct {
	ProfileID     string        `json:"profileId"`
	ProfileName   string        `json:"profileName"`
	DefaultAction string        `json:"defaultAction"`
	Entries       []PolicyEntry `json:"-"`
}
type PolicyEntry struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Kind           string            `json:"kind"`
	Provider       string            `json:"provider,omitempty"`
	Priority       int               `json:"priority"`
	Action         string            `json:"action"`
	CreatedAt      time.Time         `json:"createdAt"`
	SnapshotSHA256 string            `json:"snapshotSha256,omitempty"`
	Matchers       []DestinationRule `json:"-"`
	GeoSites       []string          `json:"-"`
	GeoIPs         []string          `json:"-"`
}

func CompilePolicy(profile RoutingProfile) (Policy, error) {
	action := profile.DefaultAction
	if action == "" {
		action = ActionVPN
	}
	if !ValidAction(action) {
		return Policy{}, errors.New("invalid profile default action")
	}
	p := Policy{ProfileID: profile.ID, ProfileName: profile.Name, DefaultAction: action}
	for _, r := range profile.Rules {
		if !r.Enabled {
			continue
		}
		if !ValidAction(r.Action) {
			return Policy{}, errors.New("invalid manual rule action")
		}
		p.Entries = append(p.Entries, PolicyEntry{ID: r.ID, Name: r.Name, Kind: "manual", Priority: r.Priority, Action: r.Action, CreatedAt: r.CreatedAt, Matchers: []DestinationRule{{Domains: r.Domains, DomainSuffixes: r.DomainSuffixes, DomainKeywords: r.DomainKeywords, IPCIDRs: r.IPCIDRs}}, GeoSites: r.GeoSites, GeoIPs: r.GeoIPs})
	}
	for _, s := range profile.ManagedSets {
		if !s.Enabled {
			continue
		}
		if s.Snapshot == nil {
			return Policy{}, errors.New("enabled managed rule set has no successful snapshot: " + s.Name)
		}
		if !ValidAction(s.Action) {
			return Policy{}, errors.New("invalid managed rule action")
		}
		p.Entries = append(p.Entries, PolicyEntry{ID: s.ID, Name: s.Name, Kind: "managed", Provider: s.Provider, Priority: s.Priority, Action: s.Action, CreatedAt: s.CreatedAt, SnapshotSHA256: s.SnapshotSHA256, Matchers: s.Snapshot.Rules})
	}
	sort.SliceStable(p.Entries, func(i, j int) bool {
		a, b := p.Entries[i], p.Entries[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	return p, nil
}
func outboundFor(action, vpnTag string) string {
	if action == ActionVPN {
		return vpnTag
	}
	return action
}
func (p Policy) Render(vpnTag string) (rules []map[string]any, sets []map[string]any, final string) {
	for _, e := range p.Entries {
		var rule map[string]any
		if e.Kind == "managed" {
			tag := "managed-" + e.ID
			sets = append(sets, map[string]any{"type": "inline", "tag": tag, "rules": e.Matchers})
			rule = map[string]any{"rule_set": []string{tag}}
		} else {
			m := e.Matchers[0]
			rule = map[string]any{}
			putList(rule, "domain", m.Domains)
			putList(rule, "domain_suffix", m.DomainSuffixes)
			putList(rule, "domain_keyword", m.DomainKeywords)
			putList(rule, "ip_cidr", m.IPCIDRs)
			putList(rule, "geosite", e.GeoSites)
			putList(rule, "geoip", e.GeoIPs)
			if len(rule) == 0 {
				continue
			}
		}
		if e.Action == ActionBlock {
			rule["action"] = "reject"
		} else {
			rule["outbound"] = outboundFor(e.Action, vpnTag)
		}
		rules = append(rules, rule)
	}
	final = outboundFor(p.DefaultAction, vpnTag)
	if p.DefaultAction == ActionBlock {
		rules = append(rules, map[string]any{"action": "reject"})
		final = vpnTag
	}
	return
}
func putList(m map[string]any, k string, v []string) {
	if len(v) > 0 {
		m[k] = v
	}
}

type DiagnosticResult struct {
	ProfileID   string       `json:"profileId"`
	ProfileName string       `json:"profileName"`
	Destination string       `json:"destination"`
	ResolvedIP  string       `json:"resolvedIp,omitempty"`
	Action      string       `json:"action,omitempty"`
	Status      string       `json:"status"`
	Winner      *PolicyEntry `json:"winner,omitempty"`
	Order       int          `json:"order,omitempty"`
	Reason      string       `json:"reason"`
}

// Diagnose uses precisely the entries emitted by Render. It does not perform DNS
// on the Manager: that would misrepresent the client's resolver. Missing runtime
// metadata is reported as indeterminate instead of guessing a winning action.
func (p Policy) Diagnose(destination, resolvedIP string) (DiagnosticResult, error) {
	destination = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(destination), "."))
	ip, ipErr := netip.ParseAddr(destination)
	domain := destination
	if ipErr == nil {
		domain = ""
		ip = ip.Unmap()
	} else if !validDomainMatcher(destination, false) {
		return DiagnosticResult{}, errors.New("enter a domain (ASCII/punycode) or IP address")
	}
	if resolvedIP != "" {
		if domain == "" {
			return DiagnosticResult{}, errors.New("resolvedIp is only accepted with a domain")
		}
		var err error
		ip, err = netip.ParseAddr(resolvedIP)
		if err != nil {
			return DiagnosticResult{}, errors.New("invalid resolvedIp")
		}
		ip = ip.Unmap()
	}
	result := DiagnosticResult{ProfileID: p.ProfileID, ProfileName: p.ProfileName, Destination: destination, ResolvedIP: resolvedIP, Status: "matched"}
	for i, e := range p.Entries {
		matched, unknown := false, false
		for _, m := range e.Matchers {
			hit, uncertain := m.match(domain, ip)
			matched = matched || hit
			unknown = unknown || uncertain
		}
		if len(e.GeoSites) > 0 || len(e.GeoIPs) > 0 {
			unknown = true
		}
		if !matched && !unknown {
			continue
		}
		result.Winner = &e
		result.Order = i + 1
		if unknown && (!matched || len(e.GeoSites) > 0 || len(e.GeoIPs) > 0) {
			result.Status = "indeterminate"
			result.Reason = "runtime_metadata_required"
			return result, nil
		}
		result.Action = e.Action
		result.Reason = "first_match_priority_created_id"
		return result, nil
	}
	result.Action = p.DefaultAction
	result.Status = "default"
	result.Reason = "profile_default"
	return result, nil
}
func (r DestinationRule) match(domain string, ip netip.Addr) (bool, bool) {
	if domain != "" {
		for _, v := range r.Domains {
			if domain == strings.ToLower(v) {
				return true, false
			}
		}
		for _, v := range r.DomainSuffixes {
			v = strings.ToLower(v)
			// A leading dot matches subdomains only (sing-box 1.9+).
			if strings.HasPrefix(v, ".") {
				if strings.HasSuffix(domain, v) {
					return true, false
				}
			} else if domain == v || strings.HasSuffix(domain, "."+v) {
				return true, false
			}
		}
		for _, v := range r.DomainKeywords {
			if strings.Contains(domain, strings.ToLower(v)) {
				return true, false
			}
		}
		for _, v := range r.DomainRegex {
			if ok, _ := regexp.MatchString(v, domain); ok {
				return true, false
			}
		}
	}
	for _, v := range r.IPCIDRs {
		if prefix, err := netip.ParsePrefix(v); err == nil && ip.IsValid() && prefix.Contains(ip) {
			return true, false
		}
	}
	return false, len(r.IPCIDRs) > 0 && !ip.IsValid()
}
