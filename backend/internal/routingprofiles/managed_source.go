package routingprofiles

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxSourceBytes = 16 << 20

// DestinationRule is the supported, strictly validated subset of sing-box headless rules.
// Domain matchers and destination CIDRs belong to the SAME OR group in sing-box.
type DestinationRule struct {
	Domains        []string `json:"domain,omitempty"`
	DomainSuffixes []string `json:"domain_suffix,omitempty"`
	DomainKeywords []string `json:"domain_keyword,omitempty"`
	DomainRegex    []string `json:"domain_regex,omitempty"`
	IPCIDRs        []string `json:"ip_cidr,omitempty"`
}
type SourceDocument struct {
	Version int               `json:"version"`
	Rules   []DestinationRule `json:"rules"`
}
type SourceProvider struct {
	Format string `json:"format"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Action string `json:"action"`
}

// Providers are metadata, independent from the source decoder and refresh lifecycle.
// A custom URL can use the same supported format without a new provider implementation.
var SourceProviders = []SourceProvider{
	{"domain-list", "refilter", "Re:filter", "https://raw.githubusercontent.com/1andrevich/Re-filter-lists/main/domains_all.lst", ActionVPN},
	{"domain-list", "runetfreedom", "RunetFreedom", "https://raw.githubusercontent.com/runetfreedom/russia-blocked-geosite/release/ru-blocked.txt", ActionVPN},
	{"sing-box-source", "custom", "Custom HTTPS source", "", ActionVPN},
}

func ParseSource(data []byte) (SourceDocument, error) {
	var doc SourceDocument
	if len(data) > maxSourceBytes {
		return doc, errors.New("source exceeds 16 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return SourceDocument{}, fmt.Errorf("invalid sing-box source: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return SourceDocument{}, errors.New("source must contain exactly one JSON document")
	}
	if doc.Version < 1 || doc.Version > 3 || len(doc.Rules) == 0 || len(doc.Rules) > 100000 {
		return SourceDocument{}, errors.New("source requires version 1–3 and 1–100000 rules")
	}
	total := 0
	for i := range doc.Rules {
		r := &doc.Rules[i]
		n := len(r.Domains) + len(r.DomainSuffixes) + len(r.DomainKeywords) + len(r.DomainRegex) + len(r.IPCIDRs)
		total += n
		if n == 0 || total > 500000 {
			return SourceDocument{}, errors.New("source contains an empty rule or more than 500000 matchers")
		}
		if err := validateSourceMatchers(*r); err != nil {
			return SourceDocument{}, err
		}
		for _, v := range r.DomainRegex {
			if v == "" || len(v) > 2048 {
				return SourceDocument{}, errors.New("domain regex is too long")
			}
			if _, err := regexp.Compile(v); err != nil {
				return SourceDocument{}, errors.New("invalid domain regex")
			}
		}
		for j, v := range r.IPCIDRs {
			if addr, err := netip.ParseAddr(v); err == nil {
				r.IPCIDRs[j] = netip.PrefixFrom(addr, addr.BitLen()).String()
			} else if _, err := netip.ParsePrefix(v); err != nil {
				return SourceDocument{}, errors.New("invalid destination IP/CIDR")
			}
		}
		for _, group := range [][]string{r.Domains, r.DomainSuffixes, r.DomainKeywords} {
			for j := range group {
				group[j] = strings.ToLower(group[j])
			}
		}
	}
	return doc, nil
}

func validateSourceURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 2048 || (u.Port() != "" && u.Port() != "443") {
		return errors.New("source URL must use HTTPS on port 443 without credentials or fragment")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicSourceIP(ip) {
		return errors.New("source address must be public")
	}
	return nil
}
func publicSourceIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, s := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "2002::/16"} {
		if netip.MustParsePrefix(s).Contains(ip) {
			return false
		}
	}
	return true
}

func newSourceClient() *http.Client {
	transport := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxIdleConns: 4, IdleConnTimeout: time.Minute}
	// Resolve and pin the actual dial address, including every redirect: no DNS rebinding,
	// private address access or proxy environment fallback.
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, errors.New("source DNS resolution failed")
		}
		if len(ips) == 0 {
			return nil, errors.New("source has no IP addresses")
		}
		for _, ip := range ips {
			if !publicSourceIP(ip) {
				return nil, errors.New("source resolves to a non-public address")
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
			err = e
		}
		return nil, err
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many source redirects")
		}
		return validateSourceURL(req.URL.String())
	}}
}

type SourceFetcher interface {
	Fetch(context.Context, string, string) (SourceDocument, error)
}
type HTTPSourceFetcher struct{ client *http.Client }

func NewSourceFetcher() *HTTPSourceFetcher { return &HTTPSourceFetcher{client: newSourceClient()} }
func (f *HTTPSourceFetcher) Fetch(ctx context.Context, sourceURL, provider string) (SourceDocument, error) {
	if err := validateSourceURL(sourceURL); err != nil {
		return SourceDocument{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return SourceDocument{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "RouteGate-ManagedRules/1")
	response, err := f.client.Do(req)
	if err != nil {
		return SourceDocument{}, errors.New("source download failed (network, TLS, timeout or address policy)")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return SourceDocument{}, fmt.Errorf("source returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxSourceBytes+1))
	if err != nil {
		return SourceDocument{}, errors.New("source response read failed")
	}
	for _, p := range SourceProviders {
		if p.ID != provider {
			continue
		}
		switch p.Format {
		case "domain-list":
			return parseDomainSource(data)
		case "sing-box-source":
			return ParseSource(data)
		}
	}
	return SourceDocument{}, errors.New("unsupported source provider or format")
}

// Text adapters convert upstream destination lists into modern sing-box headless
// rules, never into GeoSite/GeoIP database references. Includes and attributes are
// rejected because silently discarding them would change routing semantics.
func parseDomainSource(data []byte) (SourceDocument, error) {
	if len(data) > maxSourceBytes {
		return SourceDocument{}, errors.New("source exceeds 16 MiB")
	}
	rule := DestinationRule{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 4096)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		kind, value, ok := strings.Cut(line, ":")
		if !ok {
			kind = "domain"
			value = line
		}
		value = strings.TrimSpace(value)
		switch kind {
		case "domain":
			rule.DomainSuffixes = append(rule.DomainSuffixes, strings.TrimSuffix(value, "."))
		case "full":
			rule.Domains = append(rule.Domains, strings.TrimSuffix(value, "."))
		case "keyword":
			rule.DomainKeywords = append(rule.DomainKeywords, value)
		case "regexp":
			rule.DomainRegex = append(rule.DomainRegex, value)
		default:
			return SourceDocument{}, errors.New("unsupported domain-list directive: " + kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return SourceDocument{}, errors.New("invalid or oversized domain-list line")
	}
	raw, err := json.Marshal(SourceDocument{Version: 1, Rules: []DestinationRule{rule}})
	if err != nil {
		return SourceDocument{}, err
	}
	return ParseSource(raw)
}
func validateSourceMatchers(r DestinationRule) error {
	// sing-box also permits a single DNS label (e.g. a TLD suffix).
	for _, group := range [][]string{r.Domains, r.DomainSuffixes} {
		for _, v := range group {
			if len(v) > 253 {
				return errors.New("source domain is too long")
			}
			v = strings.TrimPrefix(v, ".")
			for _, label := range strings.Split(v, ".") {
				if !validDomainLabel(label) {
					return errors.New("invalid source domain")
				}
			}
		}
	}
	return validateLooseMatchers("domain_keyword", r.DomainKeywords, 253)
}
