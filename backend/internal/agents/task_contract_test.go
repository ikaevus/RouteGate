package agents

import "testing"

func TestAgentConfigTaskEffectiveKindDefaultsToConfigApply(t *testing.T) {
	if got := (AgentConfigTask{}).EffectiveKind(); got != AgentTaskKindConfigApply {
		t.Fatalf("effective kind = %q", got)
	}
}

func TestAgentConfigTaskEffectiveKindPreservesVPNCoreService(t *testing.T) {
	task := AgentConfigTask{Kind: AgentTaskKindVPNCoreService, Operation: VPNCoreOperationRestart}
	if got := task.EffectiveKind(); got != AgentTaskKindVPNCoreService {
		t.Fatalf("effective kind = %q", got)
	}
	if task.Operation != VPNCoreOperationRestart {
		t.Fatalf("operation = %q", task.Operation)
	}
}

func TestAgentConfigTaskCarriesExplicitVPNCoreInstallationOperation(t *testing.T) {
	task := AgentConfigTask{Kind: AgentTaskKindVPNCoreInstall, Operation: VPNCoreOperationInstallSingBox}
	if task.EffectiveKind() != AgentTaskKindVPNCoreInstall {
		t.Fatalf("kind = %q", task.EffectiveKind())
	}
	if task.Operation != VPNCoreOperationInstallSingBox {
		t.Fatalf("operation = %q", task.Operation)
	}
}

func TestOperationCapabilityAllowsOnlyKnownKindOperationPairs(t *testing.T) {
	if capability, err := operationCapability(AgentTaskKindVPNCoreInstall, VPNCoreOperationInstallSingBox); err != nil || capability != "vpnCoreInstallationOperations" {
		t.Fatalf("installation capability = %q, err=%v", capability, err)
	}
	for _, test := range []struct{ kind, operation string }{
		{AgentTaskKindVPNCoreInstall, "xray"},
		{AgentTaskKindVPNCoreInstall, VPNCoreOperationStart},
		{AgentTaskKindVPNCoreService, VPNCoreOperationInstallSingBox},
		{"shell", "sh -c"},
	} {
		if _, err := operationCapability(test.kind, test.operation); err == nil {
			t.Fatalf("accepted kind=%q operation=%q", test.kind, test.operation)
		}
	}
}
