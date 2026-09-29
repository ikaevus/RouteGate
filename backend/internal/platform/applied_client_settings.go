package platform

// AppliedClientSettings is the client-facing view of what a rendered config
// version deploys on a node. Manager derives it from each version's rendered
// config so that client material (subscriptions, connection links) follows
// the last Agent-confirmed apply instead of settings that were saved but not
// yet deployed, or that the node runtime does not honour at all.
//
// The WireGuard DNS pushed to clients is intentionally absent: the node never
// uses it, so changing it cannot desynchronise a client from the node.
type AppliedClientSettings struct {
	VPNProtocol           string `json:"vpnProtocol"`
	VLESSPort             int    `json:"vlessPort"`
	VLESSFlow             string `json:"vlessFlow"`
	VLESSNetwork          string `json:"vlessNetwork"`
	RealityPublicKey      string `json:"realityPublicKey"`
	RealityShortID        string `json:"realityShortId"`
	RealityServerName     string `json:"realityServerName"`
	WireGuardPort         int    `json:"wireGuardPort"`
	WireGuardAddress      string `json:"wireGuardAddress"`
	WireGuardPublicKey    string `json:"wireGuardPublicKey"`
	Hysteria2Port         int    `json:"hysteria2Port"`
	Hysteria2Domain       string `json:"hysteria2Domain"`
	ShadowsocksPort       int    `json:"shadowsocksPort"`
	ShadowsocksMethod     string `json:"shadowsocksMethod"`
	ShadowsocksServerKey  string `json:"shadowsocksServerKey"`
	MTProtoPort           int    `json:"mtprotoPort"`
	MTProtoSecret         string `json:"mtprotoSecret"`
	MTProtoFrontingDomain string `json:"mtprotoFrontingDomain"`
	// Accounts maps each VPN account rendered into the version to the
	// protocols the version deploys for it. It is the source of truth for the
	// account's active protocol set whenever this version is (re)applied.
	Accounts map[string]AppliedAccountProtocols `json:"accounts,omitempty"`
}

// AppliedAccountProtocols describes what one config version deploys for one
// VPN account. Primary is empty when it is unknown (versions rendered before
// it was recorded) or not part of the deployed set.
type AppliedAccountProtocols struct {
	Primary   string   `json:"primary,omitempty"`
	Protocols []string `json:"protocols"`
}
