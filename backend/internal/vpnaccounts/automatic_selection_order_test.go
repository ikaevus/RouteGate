package vpnaccounts

import "testing"

func TestOrderedUniqueServerIDsKeepsSelectedNodeFirst(t *testing.T) {
	got := orderedUniqueServerIDs("selected-node", "previous-node", "selected-node", "")
	if len(got) != 2 {
		t.Fatalf("ordered ids length = %d, want 2: %v", len(got), got)
	}
	if got[0] != "selected-node" || got[1] != "previous-node" {
		t.Fatalf("ordered ids = %v, want [selected-node previous-node]", got)
	}
}
