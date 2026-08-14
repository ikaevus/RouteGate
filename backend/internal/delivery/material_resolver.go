package delivery

import "context"

type RoutingMaterialResolver struct {
	vpnAccess          MaterialResolver
	systemNotification MaterialResolver
}

func NewRoutingMaterialResolver(vpnAccess MaterialResolver, systemNotification MaterialResolver) *RoutingMaterialResolver {
	return &RoutingMaterialResolver{vpnAccess: vpnAccess, systemNotification: systemNotification}
}

func (r *RoutingMaterialResolver) Resolve(ctx context.Context, item Delivery) (ResolvedMaterial, error) {
	switch item.TemplateKey {
	case TemplateSystemNotification:
		if r.systemNotification == nil {
			return ResolvedMaterial{}, Failure{Class: ErrorClassPermanent, Code: "notification_resolver_unavailable"}
		}
		return r.systemNotification.Resolve(ctx, item)
	case TemplateVPNAccess, TemplateVPNAccessReissued:
		if r.vpnAccess == nil {
			return ResolvedMaterial{}, Failure{Class: ErrorClassPermanent, Code: "vpn_access_resolver_unavailable"}
		}
		return r.vpnAccess.Resolve(ctx, item)
	default:
		return ResolvedMaterial{}, Failure{Class: ErrorClassPermanent, Code: "material_type_unsupported"}
	}
}
