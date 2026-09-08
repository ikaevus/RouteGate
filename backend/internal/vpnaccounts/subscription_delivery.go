package vpnaccounts

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	SubscriptionDeliveryFormatAuto      = "auto"
	SubscriptionDeliveryFormatBase64    = "base64"
	SubscriptionDeliveryFormatRaw       = "raw"
	SubscriptionDeliveryFormatSingBox   = "sing-box"
	SubscriptionDeliveryFormatWireGuard = "wireguard"
)

var errSubscriptionDeliveryFormatUnavailable = errors.New("subscription delivery format is unavailable")

type subscriptionDeliveryPayload struct {
	ContentType string
	Filename    string
	Body        string
	Protocols   []string
}

// GetClientSubscription serves the opaque user-facing subscription URL.
//
// The URL itself contains no account, node, protocol, or credential metadata;
// the bearer token is resolved server-side. The response intentionally avoids
// RouteGate's management JSON envelope so third-party VPN clients can import
// the URL directly. The existing /api/v1/subscriptions/{token} endpoint remains
// available as the RouteGate API representation.
func (h *Handler) GetClientSubscription(w http.ResponseWriter, r *http.Request) {
	setSubscriptionDeliverySecurityHeaders(w)

	rawToken := strings.TrimSpace(r.PathValue("token"))
	if rawToken == "" {
		writePublicSubscriptionNotFound(w)
		return
	}

	token, err := h.accounts.FindActiveSubscriptionTokenByHash(r.Context(), HashSubscriptionToken(rawToken))
	if errors.Is(err, pgx.ErrNoRows) {
		writePublicSubscriptionNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get client subscription token", err)
		return
	}

	now := time.Now()
	if subscriptionTokenExpired(token, now) {
		writePublicSubscriptionNotFound(w)
		return
	}

	profile, err := h.accounts.GetSubscriptionProfileByAccountID(r.Context(), token.VPNAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		writePublicSubscriptionNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get client subscription profile", err)
		return
	}
	if profile.Account.Status != StatusActive {
		writePublicSubscriptionNotFound(w)
		return
	}
	if profile.Account.ExpiresAt != nil && !profile.Account.ExpiresAt.After(now) {
		writePublicSubscriptionNotFound(w)
		return
	}
	rewriteManagedRuleSetURLs(&profile, r)

	connection, err := h.clientConnection(r.Context(), token.VPNAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		writePublicSubscriptionNotFound(w)
		return
	}
	if err != nil {
		h.logger.Warn("render client subscription failed", "vpn_account_id", token.VPNAccountID, "error", err)
		http.Error(w, "Subscription configuration is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}

	payload, err := renderSubscriptionDeliveryPayload(connection, profile, r.URL.Query().Get("format"))
	if errors.Is(err, errSubscriptionDeliveryFormatUnavailable) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err != nil {
		h.logger.Warn("render client subscription payload failed", "vpn_account_id", token.VPNAccountID, "error", err)
		http.Error(w, "Subscription configuration is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}

	if err := h.accounts.MarkSubscriptionTokenUsed(r.Context(), token.ID); err != nil {
		h.databaseError(w, "mark client subscription token used", err)
		return
	}

	w.Header().Set("Content-Type", payload.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", payload.Filename))
	w.Header().Set("Profile-Title", subscriptionProfileTitle(profile))
	if len(payload.Protocols) > 0 {
		w.Header().Set("X-RouteGate-Protocols", strings.Join(payload.Protocols, ","))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, payload.Body)
}

func renderSubscriptionDeliveryPayload(connection ClientConnectionResponse, profile SubscriptionProfile, requestedFormat string) (subscriptionDeliveryPayload, error) {
	format := strings.ToLower(strings.TrimSpace(requestedFormat))
	if format == "" {
		format = SubscriptionDeliveryFormatAuto
	}

	if format == SubscriptionDeliveryFormatAuto {
		if links, protocols := subscriptionShareLinks(connection); len(links) > 0 {
			return subscriptionDeliveryPayload{
				ContentType: "text/plain; charset=utf-8",
				Filename:    "routegate-subscription.txt",
				Body:        base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n"))),
				Protocols:   protocols,
			}, nil
		}
		format = SubscriptionDeliveryFormatRaw
	}

	switch format {
	case SubscriptionDeliveryFormatBase64:
		links, protocols := subscriptionShareLinks(connection)
		if len(links) == 0 {
			return subscriptionDeliveryPayload{}, fmt.Errorf("%w: base64 share-link subscription is not available for protocol %s", errSubscriptionDeliveryFormatUnavailable, connection.Protocol)
		}
		return subscriptionDeliveryPayload{
			ContentType: "text/plain; charset=utf-8",
			Filename:    "routegate-subscription.txt",
			Body:        base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n"))),
			Protocols:   protocols,
		}, nil
	case SubscriptionDeliveryFormatRaw:
		material, protocol := activeConnectionMaterial(connection)
		if strings.TrimSpace(material) == "" {
			return subscriptionDeliveryPayload{}, fmt.Errorf("%w: raw client material is not available for protocol %s", errSubscriptionDeliveryFormatUnavailable, connection.Protocol)
		}
		filename := "routegate-access.txt"
		if protocol == ClientProtocolWireGuard {
			filename = "routegate.conf"
		}
		return subscriptionDeliveryPayload{
			ContentType: "text/plain; charset=utf-8",
			Filename:    filename,
			Body:        material,
			Protocols:   []string{protocol},
		}, nil
	case SubscriptionDeliveryFormatWireGuard:
		for _, item := range connectionCandidates(connection) {
			if item.Protocol == ClientProtocolWireGuard && strings.TrimSpace(item.WireGuardConfig) != "" {
				return subscriptionDeliveryPayload{
					ContentType: "text/plain; charset=utf-8",
					Filename:    "routegate.conf",
					Body:        item.WireGuardConfig,
					Protocols:   []string{ClientProtocolWireGuard},
				}, nil
			}
		}
		return subscriptionDeliveryPayload{}, fmt.Errorf("%w: WireGuard config is not available", errSubscriptionDeliveryFormatUnavailable)
	case SubscriptionDeliveryFormatSingBox:
		if connection.Protocol != ClientProtocolVLESS {
			return subscriptionDeliveryPayload{}, fmt.Errorf("%w: sing-box remote profile is currently available for VLESS profiles only", errSubscriptionDeliveryFormatUnavailable)
		}
		config, err := RenderSingBoxClientConfig(profile)
		if err != nil {
			return subscriptionDeliveryPayload{}, err
		}
		encoded, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return subscriptionDeliveryPayload{}, err
		}
		return subscriptionDeliveryPayload{
			ContentType: "application/json; charset=utf-8",
			Filename:    "routegate-sing-box.json",
			Body:        string(encoded),
			Protocols:   []string{ClientProtocolVLESS},
		}, nil
	default:
		return subscriptionDeliveryPayload{}, fmt.Errorf("%w: supported formats are auto, base64, raw, sing-box, and wireguard", errSubscriptionDeliveryFormatUnavailable)
	}
}

func subscriptionShareLinks(connection ClientConnectionResponse) ([]string, []string) {
	links := make([]string, 0)
	protocols := make([]string, 0)
	seen := map[string]struct{}{}

	for _, item := range connectionCandidates(connection) {
		var link string
		switch item.Protocol {
		case ClientProtocolVLESS:
			link = item.VLESSLink
		case ClientProtocolHysteria2:
			link = item.Hysteria2URI
		case ClientProtocolShadowsocks:
			link = item.ShadowsocksURI
		default:
			continue
		}
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}
		links = append(links, link)
		protocols = append(protocols, item.Protocol)
	}
	return links, protocols
}

func connectionCandidates(connection ClientConnectionResponse) []ClientProtocolConnection {
	if len(connection.Connections) > 0 {
		return connection.Connections
	}
	return []ClientProtocolConnection{{
		Protocol:        connection.Protocol,
		Format:          connection.Format,
		VLESSLink:       connection.VLESSLink,
		WireGuardConfig: connection.WireGuardConfig,
		Hysteria2URI:    connection.Hysteria2URI,
		ShadowsocksURI:  connection.ShadowsocksURI,
		MTProtoURI:      connection.MTProtoURI,
	}}
}

func activeConnectionMaterial(connection ClientConnectionResponse) (string, string) {
	switch connection.Protocol {
	case ClientProtocolVLESS:
		return connection.VLESSLink, ClientProtocolVLESS
	case ClientProtocolWireGuard:
		return connection.WireGuardConfig, ClientProtocolWireGuard
	case ClientProtocolHysteria2:
		return connection.Hysteria2URI, ClientProtocolHysteria2
	case ClientProtocolShadowsocks:
		return connection.ShadowsocksURI, ClientProtocolShadowsocks
	case ClientProtocolMTProto:
		return connection.MTProtoURI, ClientProtocolMTProto
	default:
		return "", connection.Protocol
	}
}

func subscriptionProfileTitle(profile SubscriptionProfile) string {
	title := strings.TrimSpace(profile.Account.DisplayName)
	if title == "" {
		title = "RouteGate"
	}
	return base64.StdEncoding.EncodeToString([]byte(title))
}

func setSubscriptionDeliverySecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
}
