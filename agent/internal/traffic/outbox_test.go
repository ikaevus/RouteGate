package traffic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOutboxPreservesPendingBatchAndBaselineAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	box, err := OpenOutbox(path, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	sample := []CounterSnapshot{{VPNAccountID: "a", RxBytes: 100, TxBytes: 200, ObservedAt: time.Now().UTC()}}
	if err := box.Prepare(sample); err != nil {
		t.Fatal(err)
	}
	sample[0].RxBytes = 180
	sample[0].TxBytes = 260
	if err := box.Prepare(sample); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(box.Pending)
	restarted, err := OpenOutbox(path, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	sample[0].RxBytes = 999
	if err := restarted.Prepare(sample); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(restarted.Pending)
	if string(before) != string(after) || restarted.StreamID != box.StreamID || restarted.Sequence != 1 {
		t.Fatal("pending batch changed before acknowledgement")
	}
	if len(restarted.Pending) != 1 || restarted.Pending[0].RxBytes != 80 || restarted.Pending[0].TxBytes != 60 {
		t.Fatal(restarted.Pending)
	}
	if err := restarted.Acknowledge(); err != nil {
		t.Fatal(err)
	}
	committed, err := OpenOutbox(path, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if committed.Sequence != 2 || len(committed.Pending) != 0 {
		t.Fatal("acknowledgement not persisted")
	}
	if err := committed.Prepare(sample); err != nil {
		t.Fatal(err)
	}
	if len(committed.Pending) != 1 || committed.Pending[0].RxBytes != 819 {
		t.Fatal(committed.Pending)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}

func TestOutboxFailsClosedOnCorruptionIdentityMismatchAndWriteFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	box, err := OpenOutbox(path, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := box.Prepare(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenOutbox(path, "agent-2"); err == nil {
		t.Fatal("accepted another Agent's state")
	}
	if err := os.WriteFile(path, []byte("bad json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenOutbox(path, "agent-1"); err == nil {
		t.Fatal("silently replaced corrupt checkpoint")
	}
	missing := filepath.Join(t.TempDir(), "missing", "state.json")
	box, err = OpenOutbox(missing, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := box.Prepare([]CounterSnapshot{{VPNAccountID: "a", RxBytes: 1}}); err == nil {
		t.Fatal("accepted unsaved checkpoint")
	}
	if len(box.Last) != 0 {
		t.Fatal("advanced baseline after persistence failure")
	}
}
