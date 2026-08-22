package delivery

import (
	"net/url"
	"strings"
	"unicode"

	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

func buildProtocolAccessBundle(connection vpnaccounts.ClientConnectionResponse, locale string) (ProtocolAccessBundle, error) {
	protocol, material, err := clientConnectionAccessMaterial(connection)
	if err != nil {
		return ProtocolAccessBundle{}, err
	}
	locale = strings.ToLower(strings.TrimSpace(locale))
	bundle := ProtocolAccessBundle{Protocol: protocol}

	switch protocol {
	case vpnaccounts.ClientProtocolVLESS:
		bundle.DisplayName = "VLESS / Reality"
		bundle.URI = material
		bundle.QRPayload = material
	case vpnaccounts.ClientProtocolShadowsocks:
		bundle.DisplayName = "Shadowsocks 2022"
		bundle.URI = material
		bundle.QRPayload = material
	case vpnaccounts.ClientProtocolWireGuard:
		bundle.DisplayName = "WireGuard"
		bundle.ConfigText = material
		bundle.ConfigFilename = wireGuardConfigFilename(connection.Profile.Name)
		bundle.QRPayload = material
	case vpnaccounts.ClientProtocolMTProto:
		bundle.DisplayName = "MTProto / FakeTLS"
		bundle.URI = material
		bundle.AlternativeURI = mtprotoHTTPSURL(material)
		bundle.QRPayload = material
	case vpnaccounts.ClientProtocolHysteria2:
		bundle.DisplayName = "Hysteria2"
		bundle.URI = material
		bundle.QRPayload = material
	default:
		return ProtocolAccessBundle{}, Failure{Class: ErrorClassPermanent, Code: "access_protocol_unsupported"}
	}

	bundle.PrimaryAction, bundle.ClientHint = protocolAccessCopy(locale, protocol)
	return bundle, nil
}

func protocolAccessCopy(locale, protocol string) (string, string) {
	if locale == "ru" {
		switch protocol {
		case vpnaccounts.ClientProtocolVLESS:
			return "Импортируйте ссылку VLESS в совместимый VPN-клиент.", "Если клиент поддерживает QR, можно отсканировать приложенный код."
		case vpnaccounts.ClientProtocolShadowsocks:
			return "Импортируйте ссылку Shadowsocks в совместимый клиент.", "Используйте клиент с поддержкой Shadowsocks 2022."
		case vpnaccounts.ClientProtocolWireGuard:
			return "Импортируйте конфигурацию WireGuard.", "Откройте .conf файл в WireGuard или импортируйте конфигурацию через QR-код."
		case vpnaccounts.ClientProtocolMTProto:
			return "Откройте ссылку прокси в Telegram.", "Telegram предложит добавить этот MTProto-прокси в настройки подключения."
		case vpnaccounts.ClientProtocolHysteria2:
			return "Импортируйте ссылку Hysteria2 в совместимый клиент.", "Используйте клиент с поддержкой Hysteria2."
		}
	}
	switch protocol {
	case vpnaccounts.ClientProtocolVLESS:
		return "Import the VLESS link into a compatible VPN client.", "If your client supports QR import, you can scan the attached code."
	case vpnaccounts.ClientProtocolShadowsocks:
		return "Import the Shadowsocks link into a compatible client.", "Use a client with Shadowsocks 2022 support."
	case vpnaccounts.ClientProtocolWireGuard:
		return "Import the WireGuard configuration.", "Open the .conf file in WireGuard or import the configuration using the QR code."
	case vpnaccounts.ClientProtocolMTProto:
		return "Open the proxy link in Telegram.", "Telegram will offer to add this MTProto proxy to its connection settings."
	case vpnaccounts.ClientProtocolHysteria2:
		return "Import the Hysteria2 link into a compatible client.", "Use a client with Hysteria2 support."
	default:
		return "Import the connection material into a compatible client.", ""
	}
}

func mtprotoHTTPSURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "tg" || parsed.Host != "proxy" {
		return ""
	}
	query := parsed.Query()
	server := strings.TrimSpace(query.Get("server"))
	port := strings.TrimSpace(query.Get("port"))
	secret := strings.TrimSpace(query.Get("secret"))
	if server == "" || port == "" || secret == "" {
		return ""
	}
	values := url.Values{}
	values.Set("server", server)
	values.Set("port", port)
	values.Set("secret", secret)
	return "https://t.me/proxy?" + values.Encode()
}

func wireGuardConfigFilename(profileName string) string {
	name := strings.TrimSpace(profileName)
	if name == "" {
		name = "routegate-wireguard"
	}
	var out strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			out.WriteByte('-')
			lastDash = true
		}
	}
	cleaned := strings.Trim(out.String(), "-")
	if cleaned == "" {
		cleaned = "routegate-wireguard"
	}
	return cleaned + ".conf"
}
