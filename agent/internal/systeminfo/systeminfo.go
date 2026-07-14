package systeminfo

import (
	"os"
	"os/exec"
	"runtime"

	"github.com/ikaevus/routegate/agent/internal/buildinfo"
)

type Info struct {
	Hostname        string         `json:"hostname"`
	AgentVersion    string         `json:"agentVersion"`
	ProtocolVersion int            `json:"protocolVersion"`
	OS              string         `json:"os"`
	Arch            string         `json:"arch"`
	Capabilities    map[string]any `json:"capabilities"`
}

func Collect() Info {
	hostname, _ := os.Hostname()
	info := buildinfo.Current()
	return Info{Hostname: hostname, AgentVersion: info.Version, ProtocolVersion: info.ProtocolVersion, OS: runtime.GOOS, Arch: runtime.GOARCH, Capabilities: DetectCapabilities()}
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
