package vpnaccounts

import (
	"errors"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

// ErrNodeConfigNotApplied reports a node whose saved settings have never been
// confirmed by an Agent apply, so clients cannot connect to it yet.
var ErrNodeConfigNotApplied = errors.New("the VPN node configuration has not been applied yet; render and apply it in the node's Deployments before sharing client access")

// useAppliedClientSettings replaces the node parameters that clients must match
// with those of the last successfully applied config version. Settings saved
// after that apply stay invisible to clients until a later apply succeeds, so
// a rejected or failed apply cannot hand clients parameters the running node
// does not use.
func (s *SubscriptionServer) useAppliedClientSettings(applied platform.AppliedClientSettings) {
	s.VLESSPort = applied.VLESSPort
	if s.VLESSPort <= 0 {
		s.VLESSPort = defaultSingBoxServerPort
	}
	s.VLESSFlow = applied.VLESSFlow
	s.RealityPublicKey = applied.RealityPublicKey
	s.RealityShortID = applied.RealityShortID
	s.RealityServerName = applied.RealityServerName
	s.WireGuardPort = applied.WireGuardPort
	s.WireGuardAddress = applied.WireGuardAddress
	s.WireGuardPublicKey = applied.WireGuardPublicKey
	s.Hysteria2Port = applied.Hysteria2Port
	s.Hysteria2Domain = applied.Hysteria2Domain
	s.ShadowsocksPort = applied.ShadowsocksPort
	s.ShadowsocksServerKey = applied.ShadowsocksServerKey
	s.MTProtoPort = applied.MTProtoPort
	s.MTProtoSecret = applied.MTProtoSecret
}
