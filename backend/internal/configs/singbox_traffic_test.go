package configs

import "testing"

func TestSingBoxTrafficRequiresAgentOptInAndRuntimeSupport(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, supported := range []bool{false, true} {
			cfg := RenderedConfig{SingBox: SingBoxConfig{Inbounds: []map[string]any{{"users": []map[string]any{{"name": "account-1"}, {"name": "account-1"}}}}}}
			info := ServerConfigInfo{Agent: &AgentConfigInfo{Capabilities: map[string]any{
				"trafficAccounting": map[string]any{"enabled": enabled, "source": "sing-box-v2ray-api"},
				"vpnCore":           map[string]any{"v2rayAPI": supported},
			}}}
			configureSingBoxTraffic(&cfg, info)
			if (cfg.SingBox.Experimental != nil) != (enabled && supported) {
				t.Fatalf("enabled=%t supported=%t: %+v", enabled, supported, cfg.SingBox.Experimental)
			}
			if enabled && supported {
				api := cfg.SingBox.Experimental["v2ray_api"].(map[string]any)
				if api["listen"] != "127.0.0.1:18085" {
					t.Fatal(api)
				}
				if len(api["stats"].(map[string]any)["users"].([]string)) != 1 {
					t.Fatal(api)
				}
			}
		}
	}
}
