package traffic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const FileCollectorSource = "routegate_agent_file_collector"

type CounterSnapshot struct {
	VPNAccountID string         `json:"vpnAccountId"`
	RxBytes      int64          `json:"rxBytes"`
	TxBytes      int64          `json:"txBytes"`
	ObservedAt   time.Time      `json:"observedAt"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type UsageEvent struct {
	VPNAccountID string         `json:"vpnAccountId"`
	RxBytes      int64          `json:"rxBytes"`
	TxBytes      int64          `json:"txBytes"`
	ObservedAt   time.Time      `json:"observedAt"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type Collector interface {
	Collect(ctx context.Context) ([]CounterSnapshot, error)
}

type NoopCollector struct{}

func (NoopCollector) Collect(ctx context.Context) ([]CounterSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

type FileCollector struct {
	path string
	now  func() time.Time
}

func NewFileCollector(path string) *FileCollector {
	return &FileCollector{path: strings.TrimSpace(path), now: time.Now}
}

func (c *FileCollector) Collect(ctx context.Context) ([]CounterSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read traffic usage file: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}

	snapshots, err := decodeSnapshots(data)
	if err != nil {
		return nil, err
	}

	now := c.now().UTC()
	for index := range snapshots {
		normalized, err := normalizeSnapshot(snapshots[index], index, now)
		if err != nil {
			return nil, err
		}
		snapshots[index] = normalized
	}

	return snapshots, nil
}

func decodeSnapshots(data []byte) ([]CounterSnapshot, error) {
	var payload struct {
		Counters []CounterSnapshot `json:"counters"`
		Events   []CounterSnapshot `json:"events"`
	}
	if err := json.Unmarshal(data, &payload); err == nil {
		if payload.Counters != nil {
			return payload.Counters, nil
		}
		if payload.Events != nil {
			return payload.Events, nil
		}
	}

	var snapshots []CounterSnapshot
	if err := json.Unmarshal(data, &snapshots); err != nil {
		return nil, fmt.Errorf("parse traffic usage file: %w", err)
	}
	return snapshots, nil
}

func normalizeSnapshot(snapshot CounterSnapshot, index int, now time.Time) (CounterSnapshot, error) {
	vpnAccountID := strings.TrimSpace(snapshot.VPNAccountID)
	if vpnAccountID == "" {
		return CounterSnapshot{}, fmt.Errorf("traffic snapshot %d: vpnAccountId is required", index)
	}
	if snapshot.RxBytes < 0 || snapshot.TxBytes < 0 {
		return CounterSnapshot{}, fmt.Errorf("traffic snapshot %d: rxBytes and txBytes must be greater than or equal to zero", index)
	}
	observedAt := snapshot.ObservedAt.UTC()
	if snapshot.ObservedAt.IsZero() {
		observedAt = now
	}
	metadata := cloneMetadata(snapshot.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	if _, ok := metadata["source"]; !ok {
		metadata["source"] = FileCollectorSource
	}
	return CounterSnapshot{
		VPNAccountID: vpnAccountID,
		RxBytes:      snapshot.RxBytes,
		TxBytes:      snapshot.TxBytes,
		ObservedAt:   observedAt,
		Metadata:     metadata,
	}, nil
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	clone := make(map[string]any, len(metadata))
	for key, value := range metadata {
		clone[key] = value
	}
	return clone
}
