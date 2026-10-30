package configs

import "sort"

// Only enable the API after an opted-in Agent reports runtime build support.
// Older Agents and sing-box binaries retain their existing configuration.
func configureSingBoxTraffic(config *RenderedConfig, info ServerConfigInfo) {
	if info.Agent == nil {
		return
	}
	accounting, _ := info.Agent.Capabilities["trafficAccounting"].(map[string]any)
	core, _ := info.Agent.Capabilities["vpnCore"].(map[string]any)
	if accounting["enabled"] != true || accounting["source"] != "sing-box-v2ray-api" || core["v2rayAPI"] != true {
		return
	}
	names := map[string]bool{}
	for _, inbound := range config.SingBox.Inbounds {
		users, _ := inbound["users"].([]map[string]any)
		for _, user := range users {
			if name, ok := user["name"].(string); ok && name != "" {
				names[name] = true
			}
		}
	}
	if len(names) == 0 {
		return
	}
	users := make([]string, 0, len(names))
	for name := range names {
		users = append(users, name)
	}
	sort.Strings(users)
	if config.SingBox.Experimental == nil {
		config.SingBox.Experimental = map[string]any{}
	}
	config.SingBox.Experimental["v2ray_api"] = map[string]any{
		"listen": "127.0.0.1:18085",
		"stats":  map[string]any{"enabled": true, "users": users},
	}
}
