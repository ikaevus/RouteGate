package platform

const (
	CapabilitySchemaVersion = 1

	VPNCoreSingBox = "sing-box"
	VPNCoreWireGuard = "wireguard"

	VPNProtocolVLESS = "vless"
	VPNProtocolWireGuard = "wireguard"

	VPNTransportTCP = "tcp"
	VPNTransportUDP = "udp"

	VPNSecurityNone    = "none"
	VPNSecurityReality = "reality"
	VPNSecurityWireGuard = "wireguard"
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
		{
			Core:          VPNCoreWireGuard,
			Protocol:      VPNProtocolWireGuard,
			Transports:    []string{VPNTransportUDP},
			SecurityModes: []string{VPNSecurityWireGuard},
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
