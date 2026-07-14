package agents

import "testing"

func TestEvaluateCompatibilityWithPolicy(t *testing.T) {
	tests := []struct {
		name            string
		agentVersion    string
		protocolVersion *int
		want            string
	}{
		{name: "missing protocol is unknown", want: CompatibilityUnknown},
		{name: "old protocol requires upgrade", protocolVersion: ptrInt(0), want: CompatibilityUpgradeRequired},
		{name: "future protocol is unsupported", protocolVersion: ptrInt(3), want: CompatibilityUnsupported},
		{name: "older recommended version", agentVersion: "1.0.0", protocolVersion: ptrInt(1), want: CompatibilityUpgradeRecommended},
		{name: "dev version is safe", agentVersion: "dev", protocolVersion: ptrInt(1), want: CompatibilityCompatible},
		{name: "recommended version is compatible", agentVersion: "1.2.0", protocolVersion: ptrInt(1), want: CompatibilityCompatible},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateCompatibilityWithPolicy(tt.agentVersion, tt.protocolVersion, 1, 2, "1.2.0")
			if got.Status != tt.want {
				t.Fatalf("status = %q, want %q (%+v)", got.Status, tt.want, got)
			}
		})
	}
}

func ptrInt(value int) *int {
	return &value
}
