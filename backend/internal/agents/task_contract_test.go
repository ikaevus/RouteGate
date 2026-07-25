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
