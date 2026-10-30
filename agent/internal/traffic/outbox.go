package traffic

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Outbox persists the baseline and an immutable pending batch together before
// sending it. A lost HTTP acknowledgement therefore retries the same sequence.
type Outbox struct {
	Version  int                        `json:"version"`
	AgentID  string                     `json:"agentId"`
	StreamID string                     `json:"streamId"`
	Sequence int64                      `json:"sequence"`
	Last     map[string]CounterSnapshot `json:"last"`
	Pending  []UsageEvent               `json:"pending"`
	path     string
}

func OpenOutbox(path, agentID string) (*Outbox, error) {
	o := &Outbox{Version: 1, AgentID: agentID, Sequence: 1, Last: map[string]CounterSnapshot{}, path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, err
		}
		id[6] = (id[6] & 15) | 64
		id[8] = (id[8] & 63) | 128
		o.StreamID = fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
		return o, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<20 {
		return nil, fmt.Errorf("traffic outbox exceeds size limit")
	}
	if err := json.Unmarshal(data, o); err != nil {
		return nil, fmt.Errorf("decode traffic outbox: %w", err)
	}
	if o.Version != 1 || o.AgentID != agentID || o.Sequence < 1 || o.StreamID == "" || o.Last == nil || len(o.Pending) > 1000 {
		return nil, fmt.Errorf("invalid traffic outbox or Agent identity mismatch")
	}
	return o, nil
}

func (o *Outbox) Prepare(snapshots []CounterSnapshot) error {
	if len(o.Pending) > 0 {
		return nil
	}
	tracker := NewDeltaTracker()
	for id, snapshot := range o.Last {
		tracker.last[id] = snapshot
	}
	events := tracker.BuildUsageEvents(snapshots)
	if len(events) > 1000 {
		return fmt.Errorf("traffic batch exceeds 1000 accounts")
	}
	next := *o
	next.Last = tracker.last
	next.Pending = events
	if err := next.save(); err != nil {
		return err
	}
	*o = next
	return nil
}

func (o *Outbox) Acknowledge() error {
	if len(o.Pending) == 0 {
		return nil
	}
	next := *o
	next.Pending = nil
	next.Sequence++
	if err := next.save(); err != nil {
		return err
	}
	*o = next
	return nil
}

func (o *Outbox) save() error {
	data, err := json.Marshal(o)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(o.path), ".traffic-outbox-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, o.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(o.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
