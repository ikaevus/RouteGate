package platform

// AppliedClientSettings is the client-facing subset of a node's protocol
// settings that the node runtime actually enforces. Manager captures it with
// each rendered config version so that client material (subscriptions,
// connection links) can follow the last Agent-confirmed apply instead of
// settings that were saved but not yet deployed.
//
// Client-only values that never reach the node config (for example the
// WireGuard DNS pushed to clients) are intentionally absent: changing them
// cannot desynchronise a client from the running node.
type AppliedClientSettings struct {
	VPNProtocol          string `json:"vpnProtocol"`
	VLESSPort            int    `json:"vlessPort"`
	VLESSFlow            string `json:"vlessFlow"`
	RealityPublicKey     string `json:"realityPublicKey"`
	RealityShortID       string `json:"realityShortId"`
	RealityServerName    string `json:"realityServerName"`
	WireGuardPort        int    `json:"wireGuardPort"`
	WireGuardAddress     string `json:"wireGuardAddress"`
	WireGuardPublicKey   string `json:"wireGuardPublicKey"`
	Hysteria2Port        int    `json:"hysteria2Port"`
	Hysteria2Domain      string `json:"hysteria2Domain"`
	ShadowsocksPort      int    `json:"shadowsocksPort"`
	ShadowsocksServerKey string `json:"shadowsocksServerKey"`
	MTProtoPort          int    `json:"mtprotoPort"`
	MTProtoSecret        string `json:"mtprotoSecret"`
}
