package portal

import (
	"context"
	"errors"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

func (r *Repository) GetSubscriptionProfileByAccountID(ctx context.Context, accountID string) (vpnaccounts.SubscriptionProfile, error) {
	return vpnaccounts.NewRepository(r.pool).GetSubscriptionProfileByAccountID(ctx, accountID)
}

func (r *Repository) GetOrCreateClientProfile(ctx context.Context, accountID string) (vpnaccounts.ClientProfile, error) {
	return vpnaccounts.NewRepository(r.pool).GetOrCreateClientProfile(ctx, accountID)
}

func (r *Repository) ValidateClientProtocolTopology(ctx context.Context, serverID, protocol string) error {
	return vpnaccounts.NewRepository(r.pool).ValidateClientProtocolTopology(ctx, serverID, protocol)
}

func buildPortalDirectQRCode(
	ctx context.Context,
	source vpnaccounts.ClientConnectionSource,
	profile PortalProfile,
	locale string,
) (PortalQRCode, error) {
	response := PortalQRCode{
		ProfileID:    profile.ID,
		Available:    false,
		AccessStatus: profile.AccessStatus,
		Format:       PortalQRFormat,
	}
	if profile.AccessStatus != AccessStatusActive {
		response.Message = localizedQRCodeInactive(locale)
		return response, nil
	}
	if source == nil {
		response.Message = localizedQRCodeNotReady(locale)
		return response, nil
	}

	connection, err := vpnaccounts.BuildClientConnection(ctx, source, profile.ID)
	if err != nil {
		if errors.Is(err, vpnaccounts.ErrVPNAccountUnassigned) || errors.Is(err, vpnaccounts.ErrClientConnectionUnavailable) {
			response.Message = localizedQRCodeNotReady(locale)
			return response, nil
		}
		return PortalQRCode{}, err
	}

	material := portalConnectionMaterial(connection)
	if strings.TrimSpace(material) == "" {
		response.Message = localizedQRCodeNotReady(locale)
		return response, nil
	}

	response.Available = true
	response.QRText = material
	response.Format = connection.Format
	response.Message = localizedQRCodeWarning(locale)
	return response, nil
}

func portalConnectionMaterial(connection vpnaccounts.ClientConnectionResponse) string {
	switch strings.ToLower(strings.TrimSpace(connection.Protocol)) {
	case vpnaccounts.ClientProtocolWireGuard:
		return connection.WireGuardConfig
	case vpnaccounts.ClientProtocolHysteria2:
		return strings.TrimSpace(connection.Hysteria2URI)
	case vpnaccounts.ClientProtocolShadowsocks:
		return strings.TrimSpace(connection.ShadowsocksURI)
	case vpnaccounts.ClientProtocolMTProto:
		return strings.TrimSpace(connection.MTProtoURI)
	case vpnaccounts.ClientProtocolVLESS:
		return strings.TrimSpace(connection.VLESSLink)
	default:
		return ""
	}
}
