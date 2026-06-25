package traffic

type DeltaTracker struct {
	last map[string]CounterSnapshot
}

func NewDeltaTracker() *DeltaTracker {
	return &DeltaTracker{last: map[string]CounterSnapshot{}}
}

func (t *DeltaTracker) BuildUsageEvents(snapshots []CounterSnapshot) []UsageEvent {
	if t.last == nil {
		t.last = map[string]CounterSnapshot{}
	}

	events := make([]UsageEvent, 0, len(snapshots))
	for _, snapshot := range snapshots {
		previous, ok := t.last[snapshot.VPNAccountID]
		t.last[snapshot.VPNAccountID] = snapshot
		if !ok {
			continue
		}

		rxDelta := snapshot.RxBytes - previous.RxBytes
		txDelta := snapshot.TxBytes - previous.TxBytes
		if rxDelta < 0 || txDelta < 0 {
			continue
		}
		if rxDelta == 0 && txDelta == 0 {
			continue
		}

		events = append(events, UsageEvent{
			VPNAccountID: snapshot.VPNAccountID,
			RxBytes:      rxDelta,
			TxBytes:      txDelta,
			ObservedAt:   snapshot.ObservedAt,
			Metadata:     snapshot.Metadata,
		})
	}

	return events
}
