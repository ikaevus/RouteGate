package delivery

import "context"

type CompositeResolver struct {
	vpnAccess          MaterialResolver
	systemNotification MaterialResolver
}

func NewCompositeResolver(vpnAccess, systemNotification MaterialResolver) *CompositeResolver {
	return &CompositeResolver{vpnAccess: vpnAccess, systemNotification: systemNotification}
}

func (r *CompositeResolver) Resolve(ctx context.Context, delivery Delivery) (ResolvedMaterial, error) {
	switch delivery.TemplateKey {
	case TemplateVPNAccess, TemplateVPNAccessReissued:
		return r.vpnAccess.Resolve(ctx, delivery)
	case TemplateSystemNotification:
		return r.systemNotification.Resolve(ctx, delivery)
	default:
		return ResolvedMaterial{}, Failure{Class: ErrorClassPermanent, Code: "unsupported_delivery_template"}
	}
}
