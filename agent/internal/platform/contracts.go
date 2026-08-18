package platform

const (
	CapabilitySchemaVersion = 1

	VPNCoreSingBox = "sing-box"

	VPNProtocolVLESS = "vless"

	VPNTransportTCP = "tcp"

	VPNSecurityNone    = "none"
	VPNSecurityReality = "reality"
)

// VPNCoreAdapterDescriptor is the Agent-side declaration of a complete
// RouteGate-managed apply lifecycle. Binary detection alone must never add an
// entry to this list.
type VPNCoreAdapterDescriptor struct {
	Core          string
	Protocol      string
	Transports    []string
	SecurityModes []string
}

func ManagedVPNCoreAdapters() []VPNCoreAdapterDescriptor {
	return []VPNCoreAdapterDescriptor{
		{
			Core:          VPNCoreSingBox,
			Protocol:      VPNProtocolVLESS,
			Transports:    []string{VPNTransportTCP},
			SecurityModes: []string{VPNSecurityNone, VPNSecurityReality},
		},
	}
}

func (d VPNCoreAdapterDescriptor) CapabilityMap() map[string]any {
	return map[string]any{
		"core":          d.Core,
		"protocol":      d.Protocol,
		"transports":    append([]string(nil), d.Transports...),
		"securityModes": append([]string(nil), d.SecurityModes...),
	}
}
