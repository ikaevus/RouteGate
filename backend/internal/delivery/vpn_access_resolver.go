package delivery

import (
	"context"
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
	connectURL, err := BuildConnectURL(r.publicURL, connection.VLESSLink)
	if err != nil {
		return ResolvedMaterial{}, err
	}

	material := ResolvedMaterial{
		TemplateData: TemplateData{
			ProfileName: strings.TrimSpace(connection.Profile.Name),
			ConnectURL:  connectURL,
		},
	}
	if delivery.AttachQR {
		png, err := RenderQRCodePNG(connection.VLESSLink)
		if err != nil {
			return ResolvedMaterial{}, err
		}
		material.Attachments = []Attachment{{
			Filename:    qrAttachmentFilename(connection.Profile.Name),
			ContentType: "image/png",
			Content:     png,
		}}
	}
	return material, nil
}

func classifyAccessMaterialError(err error) Failure {
	code := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(code, "not assigned to a server"):
		return Failure{Class: ErrorClassPermanent, Code: "vpn_server_missing"}
	case strings.Contains(code, "endpoint"):
		return Failure{Class: ErrorClassPermanent, Code: "vpn_endpoint_missing"}
	case strings.Contains(code, "reality"):
		return Failure{Class: ErrorClassPermanent, Code: "vpn_reality_incomplete"}
	case strings.Contains(code, "vless"):
		return Failure{Class: ErrorClassPermanent, Code: "vpn_access_incomplete"}
	default:
		return Failure{Class: ErrorClassTransient, Code: "vpn_access_resolution_failed"}
	}
}
