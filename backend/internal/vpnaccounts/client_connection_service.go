package vpnaccounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrVPNAccountUnassigned        = errors.New("vpn account is not assigned to a server")
	ErrClientConnectionUnavailable = errors.New("client connection is unavailable")
)

type ClientConnectionSource interface {
	GetSubscriptionProfileByAccountID(context.Context, string) (SubscriptionProfile, error)
	GetOrCreateClientProfile(context.Context, string) (ClientProfile, error)
}

func resolveEffectiveClientProtocol(profile ClientProfile, server *SubscriptionServer) string {
	preference := strings.ToLower(strings.TrimSpace(profile.Protocol))
	if preference != "" && preference != ClientProtocolAuto {
		return preference
	}
	if server == nil {
		return ClientProtocolVLESS
	}
	protocol := strings.ToLower(strings.TrimSpace(server.VPNProtocol))
	if protocol == "" || protocol == ClientProtocolAuto {
		return ClientProtocolVLESS
	}
	return protocol
}

func unavailableClientConnection(err error) (ClientConnectionResponse, error) {
	return ClientConnectionResponse{}, fmt.Errorf("%w: %v", ErrClientConnectionUnavailable, err)
}

func buildClientConnectionResponse(accountID string, subscription SubscriptionProfile, profile ClientProfile) (ClientConnectionResponse, error) {
	if subscription.Server == nil {
		return ClientConnectionResponse{}, ErrVPNAccountUnassigned
	}
	profile.ResolvedFingerprint = resolveClientFingerprint(profile)
	protocol := resolveEffectiveClientProtocol(profile, subscription.Server)

	switch protocol {
	case ClientProtocolWireGuard:
		config, err := RenderWireGuardClientConfig(subscription)
		if err != nil {
			return unavailableClientConnection(err)
		}
		return ClientConnectionResponse{
			VPNAccountID:    accountID,
			Protocol:        protocol,
			Format:          "wireguard-config",
			WireGuardConfig: config,
			Profile:         profile,
			Endpoint:        subscriptionServerEndpoint(subscription.Server),
		}, nil
	case ClientProtocolHysteria2:
		uri, err := RenderHysteria2ClientURI(subscription)
		if err != nil {
			return unavailableClientConnection(err)
		}
		return ClientConnectionResponse{
			VPNAccountID: accountID,
			Protocol:     protocol,
			Format:       "hysteria2-uri",
			Hysteria2URI: uri,
			Profile:      profile,
			Endpoint:     subscription.Server.Hysteria2Domain,
			ServerName:   subscription.Server.Hysteria2Domain,
			Network:      "quic",
		}, nil
	case ClientProtocolShadowsocks:
		uri, err := RenderShadowsocksClientURI(subscription)
		if err != nil {
			return unavailableClientConnection(err)
		}
		return ClientConnectionResponse{
			VPNAccountID:   accountID,
			Protocol:       protocol,
			Format:         "shadowsocks-uri",
			ShadowsocksURI: uri,
			Profile:        profile,
			Endpoint:       subscriptionServerEndpoint(subscription.Server),
			Network:        "tcp",
		}, nil
	case ClientProtocolMTProto:
		uri, err := RenderMTProtoClientURI(subscription)
		if err != nil {
			return unavailableClientConnection(err)
		}
		return ClientConnectionResponse{
			VPNAccountID: accountID,
			Protocol:     protocol,
			Format:       "mtproto-uri",
			MTProtoURI:   uri,
			Profile:      profile,
			Endpoint:     subscriptionServerEndpoint(subscription.Server),
			Network:      "tcp",
		}, nil
	case ClientProtocolVLESS:
		link, endpoint, serverName, network, flow, err := buildClientVLESSLink(subscription, profile)
		if err != nil {
			return unavailableClientConnection(err)
		}
		return ClientConnectionResponse{
			VPNAccountID: accountID,
			Protocol:     protocol,
			Format:       "vless-reality-uri",
			VLESSLink:    link,
			Profile:      profile,
			Endpoint:     endpoint,
			ServerName:   serverName,
			Network:      network,
			Flow:         flow,
		}, nil
	default:
		return unavailableClientConnection(fmt.Errorf("protocol %q is not supported", protocol))
	}
}

func BuildClientConnection(ctx context.Context, source ClientConnectionSource, accountID string) (ClientConnectionResponse, error) {
	if source == nil {
		return ClientConnectionResponse{}, errors.New("client connection source is unavailable")
	}
	subscription, err := source.GetSubscriptionProfileByAccountID(ctx, accountID)
	if err != nil {
		return ClientConnectionResponse{}, err
	}
	profile, err := source.GetOrCreateClientProfile(ctx, accountID)
	if err != nil {
		return ClientConnectionResponse{}, err
	}
	if err := validateClientProtocolTopologyForSource(ctx, source, subscription, profile); err != nil {
		return ClientConnectionResponse{}, err
	}
	return buildClientConnectionResponse(accountID, subscription, profile)
}
