package configs

import "github.com/ikaevus/routegate/backend/internal/platform"

// appliedClientSettings captures the node parameters that clients must match
// and that this render writes into the node runtime. The values are taken
// from the same ServerConfigInfo the render used, so a snapshot can never
// describe a configuration other than the one its version deploys.
func appliedClientSettings(info ServerConfigInfo) platform.AppliedClientSettings {
	return platform.AppliedClientSettings{
		VPNProtocol:          info.VPNProtocol,
		VLESSPort:            serverVLESSPort(info),
		VLESSFlow:            info.VLESSFlow,
		RealityPublicKey:     info.RealityPublicKey,
		RealityShortID:       info.RealityShortID,
		RealityServerName:    info.RealityServerName,
		WireGuardPort:        info.WireGuardPort,
		WireGuardAddress:     info.WireGuardAddress,
		WireGuardPublicKey:   info.WireGuardPublicKey,
		Hysteria2Port:        info.Hysteria2Port,
		Hysteria2Domain:      info.Hysteria2Domain,
		ShadowsocksPort:      info.ShadowsocksPort,
		ShadowsocksServerKey: info.ShadowsocksServerKey,
		MTProtoPort:          info.MTProtoPort,
		MTProtoSecret:        info.MTProtoSecret,
	}
}
