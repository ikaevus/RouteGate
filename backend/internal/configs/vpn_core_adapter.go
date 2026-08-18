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
	return vpnCoreAdapterRegistry{adapters: []vpnCoreAdapter{singBoxVLESSAdapter{}, wireGuardAdapter{}, hysteria2Adapter{}}}
}

func (r vpnCoreAdapterRegistry) Resolve(core, protocol, transport, security string) (vpnCoreAdapter, bool) {
	for _, adapter := range r.adapters {
		if adapter.Descriptor().Supports(core, protocol, transport, security) {
			return adapter, true
		}
	}
	return nil, false
}

func selectedVPNCoreAdapter(info ServerConfigInfo) vpnCoreAdapter {
	if info.VPNProtocol == platform.VPNProtocolHysteria2 {
		adapter, ok := defaultVPNCoreAdapterRegistry().Resolve(
			platform.VPNCoreHysteria,
			platform.VPNProtocolHysteria2,
			platform.VPNTransportQUIC,
			platform.VPNSecurityTLS,
		)
		if !ok {
			panic("default Hysteria2 adapter is not registered")
		}
		return adapter
	}
	if info.VPNProtocol == platform.VPNProtocolWireGuard {
		adapter, ok := defaultVPNCoreAdapterRegistry().Resolve(
			platform.VPNCoreWireGuard,
			platform.VPNProtocolWireGuard,
			platform.VPNTransportUDP,
			platform.VPNSecurityWireGuard,
		)
		if !ok {
			panic("default WireGuard adapter is not registered")
		}
		return adapter
	}
	return currentVLESSAdapter(realityRequested(info))
}

func currentVLESSAdapter(realityEnabled bool) vpnCoreAdapter {
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

func adapterForRenderedConfig(config RenderedConfig) (vpnCoreAdapter, bool) {
	descriptor := config.Metadata.VPNCore
	if descriptor.Core == "" {
		return currentVLESSAdapter(config.Metadata.RealityEnabled), true
	}
	return defaultVPNCoreAdapterRegistry().Resolve(descriptor.Core, descriptor.Protocol, descriptor.Transport, descriptor.Security)
}
