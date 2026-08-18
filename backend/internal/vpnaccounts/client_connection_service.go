package vpnaccounts

import (
	"context"
	"errors"
	"fmt"
)

type ClientConnectionSource interface {
	GetSubscriptionProfileByAccountID(context.Context, string) (SubscriptionProfile, error)
	GetOrCreateClientProfile(context.Context, string) (ClientProfile, error)
}

func BuildClientConnection(ctx context.Context, source ClientConnectionSource, accountID string) (ClientConnectionResponse, error) {
	if source == nil {
		return ClientConnectionResponse{}, errors.New("client connection source is unavailable")
	}
	subscription, err := source.GetSubscriptionProfileByAccountID(ctx, accountID)
	if err != nil {
		return ClientConnectionResponse{}, err
	}
	if subscription.Server == nil {
		return ClientConnectionResponse{}, fmt.Errorf("vpn account is not assigned to a server")
	}
	profile, err := source.GetOrCreateClientProfile(ctx, accountID)
	if err != nil {
		return ClientConnectionResponse{}, err
	}
	if subscription.Server.VPNProtocol == "wireguard" {
		config, renderErr := RenderWireGuardClientConfig(subscription)
		if renderErr != nil {
			return ClientConnectionResponse{}, renderErr
		}
		return ClientConnectionResponse{
			VPNAccountID: accountID,
			Format: "wireguard-config",
			WireGuardConfig: config,
			Profile: profile,
			Endpoint: subscriptionServerEndpoint(subscription.Server),
		}, nil
	}
	if subscription.Server.VPNProtocol == "hysteria2" {
		uri, renderErr := RenderHysteria2ClientURI(subscription)
		if renderErr != nil {
			return ClientConnectionResponse{}, renderErr
		}
		return ClientConnectionResponse{
			VPNAccountID: accountID,
			Format:       "hysteria2-uri",
			Hysteria2URI: uri,
			Profile:      profile,
			Endpoint:     subscription.Server.Hysteria2Domain,
			ServerName:   subscription.Server.Hysteria2Domain,
			Network:      "quic",
		}, nil
	}
	link, endpoint, serverName, network, flow, err := buildClientVLESSLink(subscription, profile)
	if err != nil {
		return ClientConnectionResponse{}, err
	}
	return ClientConnectionResponse{
		VPNAccountID: accountID,
		Format:       "vless-reality-uri",
		VLESSLink:    link,
		Profile:      profile,
		Endpoint:     endpoint,
		ServerName:   serverName,
		Network:      network,
		Flow:         flow,
	}, nil
}
