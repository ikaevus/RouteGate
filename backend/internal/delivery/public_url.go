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
	if !validProtocolAccessMaterial("vless", vlessLink) {
		return "", Failure{Class: ErrorClassPermanent, Code: "access_material_invalid"}
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(vlessLink))
	return base + "/connect.html#vless=" + payload, nil
}

func BuildProtocolConnectURL(publicURL, protocol, accessMaterial string) (string, error) {
	base, err := NormalizePublicURL(publicURL)
	if err != nil {
		return "", err
	}
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	accessMaterial = strings.TrimSpace(accessMaterial)
	if !validProtocolAccessMaterial(protocol, accessMaterial) {
		return "", Failure{Class: ErrorClassPermanent, Code: "access_material_invalid"}
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(accessMaterial))
	fragment := url.Values{
		"profile":  []string{payload},
		"protocol": []string{protocol},
	}.Encode()
	return base + "/connect.html#" + fragment, nil
}

func validProtocolAccessMaterial(protocol, accessMaterial string) bool {
	lower := strings.ToLower(strings.TrimSpace(accessMaterial))
	switch protocol {
	case "vless":
		return strings.HasPrefix(lower, "vless://")
	case "wireguard":
		return strings.Contains(accessMaterial, "[Interface]") &&
			strings.Contains(accessMaterial, "PrivateKey =") &&
			strings.Contains(accessMaterial, "[Peer]") &&
			strings.Contains(accessMaterial, "PublicKey =")
	case "hysteria2":
		return strings.HasPrefix(lower, "hysteria2://")
	case "shadowsocks":
		return strings.HasPrefix(lower, "ss://")
	case "mtproto":
		return strings.HasPrefix(lower, "tg://proxy?")
	default:
		return false
	}
}
