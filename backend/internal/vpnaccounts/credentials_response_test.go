package vpnaccounts

import "testing"

func TestAdminCredentialsExposeNewSecretsOnlyForSelectedProtocol(t *testing.T) {
	server := &SubscriptionServer{
		VPNProtocol: "vless", ShadowsocksServerKey: "server-secret", MTProtoSecret: "mtproto-secret",
	}
	response := adminCredentialsResponse(SubscriptionProfile{
		Account: Account{ID: "11111111-1111-1111-1111-111111111111"},
		Server: server,
		Credentials: SubscriptionCredentials{Shadowsocks: ShadowsocksCredentials{UserKey: "user-secret"}},
	})
	if response.Shadowsocks.ServerKey != "" || response.Shadowsocks.UserKey != "" || response.MTProto.Secret != "" {
		t.Fatalf("unselected protocol secrets leaked: %+v", response)
	}

	server.VPNProtocol = "shadowsocks"
	response = adminCredentialsResponse(SubscriptionProfile{
		Account: Account{ID: "11111111-1111-1111-1111-111111111111"},
		Server: server,
		Credentials: SubscriptionCredentials{Shadowsocks: ShadowsocksCredentials{Username: "account", UserKey: "user-secret"}},
	})
	if response.Shadowsocks.ServerKey != "server-secret" || response.Shadowsocks.UserKey != "user-secret" || response.MTProto.Secret != "" {
		t.Fatalf("unexpected Shadowsocks credentials response: %+v", response)
	}

	server.VPNProtocol = "mtproto"
	response = adminCredentialsResponse(SubscriptionProfile{Account: Account{ID: "account"}, Server: server})
	if response.MTProto.Secret != "mtproto-secret" || !response.MTProto.Shared || response.Shadowsocks.ServerKey != "" {
		t.Fatalf("unexpected MTProto credentials response: %+v", response)
	}
}
