package vpnaccounts

import (
	"encoding/json"
	"testing"
)

const testVLESSUUID = "1cf448ee-dae5-4114-814e-61c384cdce62"

func TestRenderSingBoxClientConfigStructure(t *testing.T) {
	config, err := RenderSingBoxClientConfig(SubscriptionProfile{
		Account: Account{
			ID:          "8b89d024-9558-43b0-8c3f-8149a5854225",
			DisplayName: "Alice",
			Status:      StatusActive,
			VLESSUUID:   testVLESSUUID,
		},
		Server: &SubscriptionServer{
			ID:           "server-1",
			Name:         "Finland",
			PublicIP:     "203.0.113.10",
			VLESSPort:    8443,
			VLESSNetwork: "tcp",
		},
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
	if vless.Server != "203.0.113.10" || vless.ServerPort != 8443 || vless.UUID != testVLESSUUID || vless.Network != "tcp" {
		t.Fatalf("unexpected vless outbound connection fields: %+v", vless)
	}
	if vless.TLS != nil {
		t.Fatalf("expected no TLS without Reality credentials, got %+v", vless.TLS)
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

func TestRenderSingBoxClientConfigRendersRealityTLS(t *testing.T) {
	config, err := RenderSingBoxClientConfig(SubscriptionProfile{
		Account: Account{ID: "account-1", DisplayName: "Alice", Status: StatusActive, VLESSUUID: testVLESSUUID},
		Server: &SubscriptionServer{
			ID:                "server-1",
			Name:              "Finland",
			Hostname:          "fi.routegate.example",
			VLESSPort:         443,
			VLESSFlow:         "xtls-rprx-vision",
			VLESSNetwork:      "tcp",
			RealityPublicKey:  "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0",
			RealityShortID:    "0123456789abcdef",
			RealityServerName: "www.example.com",
		},
	})
	if err != nil {
		t.Fatalf("render sing-box config: %v", err)
	}

	vless := config.Outbounds[0]
	if vless.Flow != "xtls-rprx-vision" {
		t.Fatalf("expected VLESS flow, got %q", vless.Flow)
	}
	if vless.TLS == nil {
		t.Fatal("expected TLS config")
	}
	if !vless.TLS.Enabled || vless.TLS.ServerName != "www.example.com" {
		t.Fatalf("unexpected TLS config: %+v", vless.TLS)
	}
	if vless.TLS.Reality == nil {
		t.Fatal("expected Reality TLS config")
	}
	if !vless.TLS.Reality.Enabled || vless.TLS.Reality.PublicKey != "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0" || vless.TLS.Reality.ShortID != "0123456789abcdef" {
		t.Fatalf("unexpected Reality config: %+v", vless.TLS.Reality)
	}
}

func TestRenderSingBoxClientConfigUsesHostnameWhenPublicIPMissing(t *testing.T) {
	config, err := RenderSingBoxClientConfig(SubscriptionProfile{
		Account: Account{ID: "8b89d024-9558-43b0-8c3f-8149a5854225", DisplayName: "Alice", Status: StatusActive, VLESSUUID: testVLESSUUID},
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
		Account: Account{ID: "8b89d024-9558-43b0-8c3f-8149a5854225", DisplayName: "Alice", Status: StatusActive, VLESSUUID: testVLESSUUID},
	})
	if err == nil {
		t.Fatal("expected missing server endpoint error")
	}
}

func TestRenderSingBoxClientConfigRequiresVLESSUUID(t *testing.T) {
	_, err := RenderSingBoxClientConfig(SubscriptionProfile{
		Account: Account{ID: "account-1", DisplayName: "Alice", Status: StatusActive},
		Server:  &SubscriptionServer{ID: "server-1", Name: "Finland", PublicIP: "203.0.113.10"},
	})
	if err == nil {
		t.Fatal("expected missing VLESS UUID error")
	}
}
