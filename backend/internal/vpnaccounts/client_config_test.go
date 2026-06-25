package vpnaccounts

import "testing"

func TestBuildClientConfigReturnsReadyPayload(t *testing.T) {
	config := BuildClientConfig(SubscriptionProfile{
		Account: Account{ID: "account-1", DisplayName: "Demo", Status: StatusActive},
		Server:  &SubscriptionServer{ID: "server-1", Name: "Finland", PublicIP: "203.0.113.10", Location: "Finland", Provider: "Demo"},
	})

	if config.Type != ClientConfigType || config.Status != "ready" {
		t.Fatalf("expected ready client config, got %+v", config)
	}
	if config.Payload == nil {
		t.Fatal("expected client config payload")
	}
	if config.Payload.Version != ClientConfigVersion {
		t.Fatalf("expected version %q, got %q", ClientConfigVersion, config.Payload.Version)
	}
	if config.Payload.ProfileName != "Demo - Finland" {
		t.Fatalf("unexpected profile name %q", config.Payload.ProfileName)
	}
	if config.Payload.Server.Endpoint != "203.0.113.10" {
		t.Fatalf("expected public IP endpoint, got %q", config.Payload.Server.Endpoint)
	}
	if config.Payload.Protocol.Engine != "sing-box" || config.Payload.Protocol.Status == "" {
		t.Fatalf("unexpected protocol metadata: %+v", config.Payload.Protocol)
	}
}

func TestBuildClientConfigUsesHostnameEndpointFallback(t *testing.T) {
	config := BuildClientConfig(SubscriptionProfile{
		Account: Account{ID: "account-1", DisplayName: "Demo", Status: StatusActive},
		Server:  &SubscriptionServer{ID: "server-1", Name: "Finland", Hostname: "fi.routegate.example"},
	})

	if config.Status != "ready" || config.Payload == nil {
		t.Fatalf("expected ready client config, got %+v", config)
	}
	if config.Payload.Server.Endpoint != "fi.routegate.example" {
		t.Fatalf("expected hostname endpoint fallback, got %q", config.Payload.Server.Endpoint)
	}
}

func TestBuildClientConfigRequiresServerEndpoint(t *testing.T) {
	config := BuildClientConfig(SubscriptionProfile{
		Account: Account{ID: "account-1", DisplayName: "Demo", Status: StatusActive},
	})

	if config.Type != ClientConfigType || config.Status != "pending" {
		t.Fatalf("expected pending client config, got %+v", config)
	}
	if config.Payload != nil {
		t.Fatalf("expected no payload without server endpoint, got %+v", config.Payload)
	}
	if config.Message == "" {
		t.Fatal("expected pending config message")
	}
}
