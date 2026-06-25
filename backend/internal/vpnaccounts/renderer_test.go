package vpnaccounts

import (
	"encoding/json"
	"testing"
)

func TestRenderSingBoxClientConfigStructure(t *testing.T) {
	config, err := RenderSingBoxClientConfig(SubscriptionProfile{
		Account: Account{ID: "8b89d024-9558-43b0-8c3f-8149a5854225", DisplayName: "Alice", Status: StatusActive},
		Server:  &SubscriptionServer{ID: "server-1", Name: "Finland", PublicIP: "203.0.113.10"},
	})
	if err != nil {
		t.Fatalf("render sing-box config: %v", err)
	}

	if len(config.Inbounds) != 1 {
		t.Fatalf("expected one inbound, got %d", len(config.Inbounds))
	}
	if config.Inbounds[0].Type != "mixed" || config.Inbounds[0].Listen != "127.0.0.1" || config.Inbounds[0].ListenPort != 2080 {
		t.Fatalf("unexpected mixed inbound: %+v", config.Inbounds[0])
	}

	if len(config.Outbounds) != 2 {
		t.Fatalf("expected two outbounds, got %d", len(config.Outbounds))
	}
	vless := config.Outbounds[0]
	if vless.Type != "vless" || vless.Tag != "routegate-out" {
		t.Fatalf("unexpected vless outbound identity: %+v", vless)
	}
	if vless.Server != "203.0.113.10" || vless.ServerPort != 443 || vless.UUID != "8b89d024-9558-43b0-8c3f-8149a5854225" || vless.Network != "tcp" {
		t.Fatalf("unexpected vless outbound connection fields: %+v", vless)
	}
	if config.Outbounds[1].Type != "direct" || config.Outbounds[1].Tag != "direct" {
		t.Fatalf("unexpected fallback outbound: %+v", config.Outbounds[1])
	}
	if config.Route.Final != "routegate-out" {
		t.Fatalf("expected route final routegate-out, got %q", config.Route.Final)
	}

	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal sing-box config: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal sing-box config: %v", err)
	}
	for _, key := range []string{"log", "inbounds", "outbounds", "route"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("expected marshaled config to contain %q: %s", key, string(encoded))
		}
	}
}

func TestRenderSingBoxClientConfigUsesHostnameWhenPublicIPMissing(t *testing.T) {
	config, err := RenderSingBoxClientConfig(SubscriptionProfile{
		Account: Account{ID: "8b89d024-9558-43b0-8c3f-8149a5854225", DisplayName: "Alice", Status: StatusActive},
		Server:  &SubscriptionServer{ID: "server-1", Name: "Finland", Hostname: "fi.routegate.example"},
	})
	if err != nil {
		t.Fatalf("render sing-box config: %v", err)
	}
	if config.Outbounds[0].Server != "fi.routegate.example" {
		t.Fatalf("expected hostname endpoint, got %q", config.Outbounds[0].Server)
	}
}

func TestRenderSingBoxClientConfigRequiresServerEndpoint(t *testing.T) {
	_, err := RenderSingBoxClientConfig(SubscriptionProfile{
		Account: Account{ID: "8b89d024-9558-43b0-8c3f-8149a5854225", DisplayName: "Alice", Status: StatusActive},
	})
	if err == nil {
		t.Fatal("expected missing server endpoint error")
	}
}
