package buildinfo

import "testing"

func TestCurrentUsesDevelopmentDefaults(t *testing.T) {
	info := Current()

	if info.Version != "dev" || info.GitCommit != "unknown" || info.BuildDate != "unknown" {
		t.Fatalf("unexpected defaults: %+v", info)
	}
	if info.ProtocolVersion != 1 {
		t.Fatalf("protocol version = %d, want 1", info.ProtocolVersion)
	}
}
