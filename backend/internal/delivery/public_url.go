package delivery

import (
	"encoding/base64"
	"net/url"
	"strings"
)

func NormalizePublicURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", Failure{Class: ErrorClassPermanent, Code: "public_url_missing"}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", Failure{Class: ErrorClassPermanent, Code: "public_url_invalid"}
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", Failure{Class: ErrorClassPermanent, Code: "public_url_invalid"}
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func BuildConnectURL(publicURL, vlessLink string) (string, error) {
	base, err := NormalizePublicURL(publicURL)
	if err != nil {
		return "", err
	}
	vlessLink = strings.TrimSpace(vlessLink)
	if !strings.HasPrefix(strings.ToLower(vlessLink), "vless://") {
		return "", Failure{Class: ErrorClassPermanent, Code: "access_material_invalid"}
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(vlessLink))
	return base + "/connect.html#vless=" + payload, nil
}
