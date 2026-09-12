package delivery

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/publicurl"
)

// NormalizePublicURL validates and normalizes RouteGate's configured public
// origin. This delegates to the shared internal/publicurl policy - the same
// one RG-116 device access URL issuance (vpnaccounts) uses - so there is one
// canonical definition of "RouteGate's own public URL" across packages;
// this wrapper only translates the shared package's plain errors into
// delivery's own Failure error type for existing callers.
func NormalizePublicURL(value string) (string, error) {
	normalized, err := publicurl.Normalize(value)
	if err != nil {
		if errors.Is(err, publicurl.ErrMissing) {
			return "", Failure{Class: ErrorClassPermanent, Code: "public_url_missing"}
		}
		return "", Failure{Class: ErrorClassPermanent, Code: "public_url_invalid"}
	}
	return normalized, nil
}

func BuildConnectURL(publicURL, vlessLink string) (string, error) {
	base, err := NormalizePublicURL(publicURL)
	if err != nil {
		return "", err
	}
	vlessLink = normalizeProtocolAccessMaterial("vless", vlessLink)
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
	accessMaterial = normalizeProtocolAccessMaterial(protocol, accessMaterial)
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

func normalizeProtocolAccessMaterial(protocol, accessMaterial string) string {
	if strings.EqualFold(strings.TrimSpace(protocol), "wireguard") {
		return accessMaterial
	}
	return strings.TrimSpace(accessMaterial)
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
