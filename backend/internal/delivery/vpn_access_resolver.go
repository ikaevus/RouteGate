package delivery

import (
	"context"
	"errors"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

type VPNAccessResolver struct {
	source    vpnaccounts.ClientConnectionSource
	publicURL string
}

func NewVPNAccessResolver(source vpnaccounts.ClientConnectionSource, publicURL string) *VPNAccessResolver {
	return &VPNAccessResolver{source: source, publicURL: strings.TrimSpace(publicURL)}
}

func (r *VPNAccessResolver) Resolve(ctx context.Context, delivery Delivery) (ResolvedMaterial, error) {
	switch delivery.TemplateKey {
	case TemplateVPNAccess, TemplateVPNAccessReissued:
	default:
		return ResolvedMaterial{}, Failure{Class: ErrorClassPermanent, Code: "material_type_unsupported"}
	}
	if strings.TrimSpace(delivery.VPNAccountID) == "" {
		return ResolvedMaterial{}, Failure{Class: ErrorClassPermanent, Code: "vpn_account_missing"}
	}

	connection, err := vpnaccounts.BuildClientConnection(ctx, r.source, delivery.VPNAccountID)
	if err != nil {
		return ResolvedMaterial{}, classifyAccessMaterialError(err)
	}
	bundle, err := buildProtocolAccessBundle(connection, delivery.Locale)
	if err != nil {
		return ResolvedMaterial{}, err
	}

	material := ResolvedMaterial{
		TemplateData: TemplateData{
			ProfileName: strings.TrimSpace(connection.Profile.Name),
			ConnectURL:  bundle.URI,
			Access:      bundle,
			Branding:    DefaultDeliveryBranding(delivery.Locale),
		},
	}

	if delivery.AttachQR && strings.TrimSpace(bundle.QRPayload) != "" {
		png, err := RenderQRCodePNG(bundle.QRPayload)
		if err != nil {
			return ResolvedMaterial{}, err
		}
		material.Attachments = append(material.Attachments, Attachment{
			Filename:    qrAttachmentFilename(connection.Profile.Name),
			ContentType: "image/png",
			Content:     png,
		})
	}
	if strings.EqualFold(strings.TrimSpace(delivery.Channel), "email") && bundle.ConfigText != "" && bundle.ConfigFilename != "" {
		material.Attachments = append(material.Attachments, Attachment{
			Filename:    bundle.ConfigFilename,
			ContentType: "text/plain; charset=utf-8",
			Content:     []byte(bundle.ConfigText),
		})
	}
	return material, nil
}

func clientConnectionAccessMaterial(connection vpnaccounts.ClientConnectionResponse) (string, string, error) {
	protocol := strings.ToLower(strings.TrimSpace(connection.Protocol))
	var accessMaterial string
	switch protocol {
	case vpnaccounts.ClientProtocolVLESS:
		accessMaterial = connection.VLESSLink
	case vpnaccounts.ClientProtocolWireGuard:
		accessMaterial = connection.WireGuardConfig
	case vpnaccounts.ClientProtocolHysteria2:
		accessMaterial = connection.Hysteria2URI
	case vpnaccounts.ClientProtocolShadowsocks:
		accessMaterial = connection.ShadowsocksURI
	case vpnaccounts.ClientProtocolMTProto:
		accessMaterial = connection.MTProtoURI
	default:
		return "", "", Failure{Class: ErrorClassPermanent, Code: "access_protocol_unsupported"}
	}
	accessMaterial = normalizeProtocolAccessMaterial(protocol, accessMaterial)
	if !validProtocolAccessMaterial(protocol, accessMaterial) {
		return "", "", Failure{Class: ErrorClassPermanent, Code: "access_material_invalid"}
	}
	return protocol, accessMaterial, nil
}

func classifyAccessMaterialError(err error) Failure {
	code := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case errors.Is(err, vpnaccounts.ErrVPNAccountUnassigned):
		return Failure{Class: ErrorClassPermanent, Code: "vpn_server_missing"}
	case strings.Contains(code, "endpoint"):
		return Failure{Class: ErrorClassPermanent, Code: "vpn_endpoint_missing"}
	case strings.Contains(code, "reality"):
		return Failure{Class: ErrorClassPermanent, Code: "vpn_reality_incomplete"}
	case strings.Contains(code, "vless"):
		return Failure{Class: ErrorClassPermanent, Code: "vpn_access_incomplete"}
	case errors.Is(err, vpnaccounts.ErrClientConnectionUnavailable):
		return Failure{Class: ErrorClassPermanent, Code: "vpn_access_incomplete"}
	default:
		return Failure{Class: ErrorClassTransient, Code: "vpn_access_resolution_failed"}
	}
}
