package configs

import "github.com/ikaevus/routegate/backend/internal/platform"

type vpnCoreAdapter interface {
	Descriptor() platform.VPNCoreAdapterDescriptor
	Render(*RenderedConfig, ServerConfigInfo)
	Validate(RenderedConfig, *ValidationResult)
	Ready(RenderedConfig) bool
}

type vpnCoreAdapterRegistry struct {
	adapters []vpnCoreAdapter
}

func defaultVPNCoreAdapterRegistry() vpnCoreAdapterRegistry {
	return vpnCoreAdapterRegistry{adapters: []vpnCoreAdapter{singBoxVLESSAdapter{}}}
}

func (r vpnCoreAdapterRegistry) Resolve(core, protocol, transport, security string) (vpnCoreAdapter, bool) {
	for _, adapter := range r.adapters {
		if adapter.Descriptor().Supports(core, protocol, transport, security) {
			return adapter, true
		}
	}
	return nil, false
}

func currentVPNCoreAdapter(realityEnabled bool) vpnCoreAdapter {
	security := platform.VPNSecurityNone
	if realityEnabled {
		security = platform.VPNSecurityReality
	}
	adapter, ok := defaultVPNCoreAdapterRegistry().Resolve(
		platform.VPNCoreSingBox,
		platform.VPNProtocolVLESS,
		platform.VPNTransportTCP,
		security,
	)
	if !ok {
		panic("default sing-box VLESS adapter is not registered")
	}
	return adapter
}
