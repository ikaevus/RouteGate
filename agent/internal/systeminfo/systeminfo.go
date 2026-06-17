package systeminfo

import (
	"os"
	"os/exec"
	"runtime"
)

const AgentVersion = "0.1.0"

type Info struct {
	Hostname     string         `json:"hostname"`
	AgentVersion string         `json:"agentVersion"`
	OS           string         `json:"os"`
	Arch         string         `json:"arch"`
	Capabilities map[string]any `json:"capabilities"`
}

func Collect() Info {
	hostname, _ := os.Hostname()
	return Info{Hostname: hostname, AgentVersion: AgentVersion, OS: runtime.GOOS, Arch: runtime.GOARCH, Capabilities: DetectCapabilities()}
}

func DetectCapabilities() map[string]any {
	names := []string{"systemctl", "sing-box", "xray", "nft"}
	caps := make(map[string]any, len(names))
	for _, name := range names {
		_, err := exec.LookPath(name)
		caps[name] = err == nil
	}
	return caps
}
