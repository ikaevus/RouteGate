// Package publicurl holds the single normalization policy for RouteGate's
// configured public origin, so every package that needs to issue or
// validate a canonical RouteGate URL (account-level/legacy subscription
// links, RG-116 device access links, connect.html delivery links) applies
// exactly the same rule: https only, no userinfo, no query, no fragment,
// and no path beyond the bare origin.
package publicurl

import (
	"errors"
	"net/url"
	"strings"
)

var (
	ErrMissing = errors.New("public url is not configured")
	ErrInvalid = errors.New("public url is invalid")
)

// Normalize validates value as RouteGate's configured public origin and
// returns it with any trailing slash/path stripped (e.g.
// "https://vpn.example.com"). Callers append their own path
// ("/sub/<token>", "/connect.html#...") to the result.
func Normalize(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrMissing
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrInvalid
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", ErrInvalid
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}
