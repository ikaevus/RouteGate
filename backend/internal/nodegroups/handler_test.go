package nodegroups

import "testing"

func TestValidateCreateRequiresKnownStrategy(t *testing.T) {
	err := validateCreate(CreateNodeGroupInput{Name: "EU Pool", SelectionStrategy: "random-script"})
	if err == nil {
		t.Fatal("expected unknown selection strategy to be rejected")
	}
	if err := validateCreate(CreateNodeGroupInput{Name: "EU Pool", SelectionStrategy: SelectionStrategyPriority}); err != nil {
		t.Fatalf("expected valid node group: %v", err)
	}
}

func TestValidateMemberBoundsPriorityAndWeight(t *testing.T) {
	valid := UpsertNodeGroupMemberInput{NodeGroupID: "group", ServerID: "server", Priority: 0, Weight: 1, Enabled: true}
	if err := validateMember(valid); err != nil {
		t.Fatalf("expected valid member: %v", err)
	}
	invalidWeight := valid
	invalidWeight.Weight = 0
	if err := validateMember(invalidWeight); err == nil {
		t.Fatal("expected zero weight to be rejected")
	}
	invalidPriority := valid
	invalidPriority.Priority = 10001
	if err := validateMember(invalidPriority); err == nil {
		t.Fatal("expected excessive priority to be rejected")
	}
}
