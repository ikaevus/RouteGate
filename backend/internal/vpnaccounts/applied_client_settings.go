package vpnaccounts

import (
	"errors"
	"fmt"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

// ErrNodeConfigNotApplied reports a node whose saved settings have never been
// confirmed by an Agent apply, so clients cannot connect to it yet.
var ErrNodeConfigNotApplied = errors.New("the VPN node configuration has not been applied yet; render and apply it in the node's Deployments before sharing client access")

// appliedAccountProtocolsSQL joins, for account alias a, what the node's
// active config version deploys: applied.accounts is the per-account object
// (a JSON object only when the version reliably lists its accounts) and
// applied.account the entry for a, NULL when a was not rendered.
const appliedAccountProtocolsSQL = `
	LEFT JOIN servers applied_server ON applied_server.id = a.server_id
	LEFT JOIN config_versions applied_version ON applied_version.id = applied_server.active_config_version_id
	LEFT JOIN LATERAL (
		SELECT applied_version.client_settings -> 'accounts' AS accounts,
		       applied_version.client_settings -> 'accounts' -> (a.id::text) AS account,
		       applied_version.client_settings ->> 'vpnProtocol' AS node_protocol
	) applied ON TRUE`

// appliedAccountsKnownSQL is true when the active version lists its accounts,
// so an account missing from it is known not to be deployed.
const appliedAccountsKnownSQL = `COALESCE(jsonb_typeof(applied.accounts) = 'object', FALSE)`

// appliedPrimaryProtocolSQL is the protocol a newly created client profile
// must start from: what the node's active version deploys for the account,
// never a saved but unapplied preference.
const appliedPrimaryProtocolSQL = `COALESCE(
		NULLIF(applied.account ->> 'primary', ''),
		applied.account -> 'protocols' ->> 0,
		NULLIF(applied.node_protocol, ''),
		'vless'
	)`

// useAppliedClientSettings replaces the node parameters that clients must match
// with those derived from the last successfully applied config version.
// Settings saved after that apply stay invisible to normal subscriptions until a later apply
// succeeds, and settings the node runtime never honours (such as a non-TCP
// VLESS transport) are replaced by what the node actually serves.
func (s *SubscriptionServer) useAppliedClientSettings(applied platform.AppliedClientSettings, accountID string) {
	s.deployment = &accountDeployment{
		accountsKnown: applied.Accounts != nil,
		protocols:     applied.Accounts[accountID].Protocols,
		mtproto:       applied.MTProtoSecret != "" && applied.MTProtoPort > 0,
	}
	s.VLESSPort = applied.VLESSPort
	if s.VLESSPort <= 0 {
		s.VLESSPort = defaultSingBoxServerPort
	}
	s.VLESSFlow = applied.VLESSFlow
	s.VLESSNetwork = applied.VLESSNetwork
	s.RealityPublicKey = applied.RealityPublicKey
	s.RealityShortID = applied.RealityShortID
	s.RealityServerName = applied.RealityServerName
	s.WireGuardPort = applied.WireGuardPort
	s.WireGuardAddress = applied.WireGuardAddress
	s.WireGuardPublicKey = applied.WireGuardPublicKey
	s.Hysteria2Port = applied.Hysteria2Port
	s.Hysteria2Domain = applied.Hysteria2Domain
	s.ShadowsocksPort = applied.ShadowsocksPort
	s.ShadowsocksMethod = applied.ShadowsocksMethod
	s.ShadowsocksServerKey = applied.ShadowsocksServerKey
	s.MTProtoPort = applied.MTProtoPort
	s.MTProtoSecret = applied.MTProtoSecret
	s.MTProtoFrontingDomain = applied.MTProtoFrontingDomain
}

// ErrAccountProtocolNotDeployed reports client material requested for an
// account protocol that the node's applied configuration does not serve yet.
var ErrAccountProtocolNotDeployed = errors.New("account protocol is not deployed on the node")

type accountProtocolNotDeployedError struct{ protocol string }

func (e accountProtocolNotDeployedError) Error() string {
	if e.protocol == ClientProtocolMTProto {
		return "MTProto is not running in the node's applied configuration; render and successfully apply a configuration that enables it, then retry"
	}
	return fmt.Sprintf("the node has not received this account's %s access yet; render and successfully apply a new configuration for the node, then retry", e.protocol)
}

func (accountProtocolNotDeployedError) Is(target error) bool {
	return target == ErrAccountProtocolNotDeployed
}

// awaitingNodeDeployment reports errors that only mean the node has not been
// given the requested access yet (as opposed to an inconsistent profile).
func awaitingNodeDeployment(err error) bool {
	return errors.Is(err, ErrAccountProtocolNotDeployed) || errors.Is(err, ErrNodeConfigNotApplied)
}

// accountDeployment is what the node's active config version deploys for one
// account.
type accountDeployment struct {
	// accountsKnown is false for versions whose rendered config does not
	// reliably list accounts; per-account checks are then impossible and the
	// active protocol flags alone decide, as before snapshots existed.
	accountsKnown bool
	protocols     []string
	// mtproto reports a running MTProto proxy. Its single secret is shared by
	// every account and the proxy does not enumerate accounts, so MTProto
	// access depends on the node, not on the account being rendered.
	mtproto bool
}

// requireDeployed refuses client material for a protocol the node's applied
// configuration does not serve for this account, for example an account
// created, activated or given a protocol after the last successful apply.
func (s *SubscriptionServer) requireDeployed(protocol string) error {
	if s == nil || s.deployment == nil {
		return nil
	}
	if protocol == ClientProtocolMTProto {
		if s.deployment.mtproto {
			return nil
		}
		return accountProtocolNotDeployedError{protocol: protocol}
	}
	if !s.deployment.accountsKnown || containsClientProtocol(s.deployment.protocols, protocol) {
		return nil
	}
	return accountProtocolNotDeployedError{protocol: protocol}
}

// useServerCredentials derives the node-bound credentials from p.Server.
func (p *SubscriptionProfile) useServerCredentials() {
	p.Credentials.VLESS.Flow = p.Server.VLESSFlow
	p.Credentials.VLESS.Network = p.Server.VLESSNetwork
	p.Credentials.Reality = RealityCredentials{
		PublicKey:  p.Server.RealityPublicKey,
		ShortID:    p.Server.RealityShortID,
		ServerName: p.Server.RealityServerName,
	}
}

// withSavedServerSettings returns the profile as it will be served once the
// node's saved settings are applied. Profile edits are validated against it,
// so a protocol can be selected before the render that first deploys it.
func (p SubscriptionProfile) withSavedServerSettings() SubscriptionProfile {
	if p.savedServer == nil || p.Server == nil {
		return p
	}
	saved := *p.savedServer
	p.Server = &saved
	p.useServerCredentials()
	return p
}
