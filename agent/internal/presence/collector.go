package presence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const FileCollectorSource = "routegate_agent_file_collector"

type Observation struct {
	VPNAccountID   string     `json:"vpnAccountId"`
	Protocol       string     `json:"protocol"`
	ConnectionCount int       `json:"connectionCount"`
	Source         string     `json:"source"`
	Confidence     string     `json:"confidence"`
	ConnectedAt    *time.Time `json:"connectedAt,omitempty"`
	LastActivityAt *time.Time `json:"lastActivityAt,omitempty"`
}

type Snapshot struct {
	ObservedAt time.Time     `json:"observedAt"`
	Items      []Observation `json:"items"`
}

type Collector interface { Collect(context.Context) (Snapshot, error) }

type NoopCollector struct{}

func (NoopCollector) Collect(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil { return Snapshot{}, err }
	return Snapshot{ObservedAt: time.Now().UTC(), Items: []Observation{}}, nil
}

type FileCollector struct {
	path string
	now  func() time.Time
}

func NewFileCollector(path string) *FileCollector { return &FileCollector{path: strings.TrimSpace(path), now: time.Now} }

func (c *FileCollector) Collect(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil { return Snapshot{}, err }
	now := c.now().UTC()
	empty := Snapshot{ObservedAt: now, Items: []Observation{}}
	if c.path == "" { return empty, nil }
	data, err := os.ReadFile(c.path)
	if os.IsNotExist(err) { return empty, nil }
	if err != nil { return Snapshot{}, fmt.Errorf("read client presence file: %w", err) }
	if strings.TrimSpace(string(data)) == "" { return empty, nil }
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil { return Snapshot{}, fmt.Errorf("parse client presence file: %w", err) }
	if snapshot.Items == nil { snapshot.Items = []Observation{} }
	if snapshot.ObservedAt.IsZero() { snapshot.ObservedAt = now }
	for index := range snapshot.Items {
		item := &snapshot.Items[index]
		item.VPNAccountID = strings.TrimSpace(item.VPNAccountID)
		item.Protocol = strings.TrimSpace(item.Protocol)
		item.Source = strings.TrimSpace(item.Source)
		item.Confidence = strings.TrimSpace(item.Confidence)
		if item.VPNAccountID == "" || item.Protocol == "" { return Snapshot{}, fmt.Errorf("presence item %d: vpnAccountId and protocol are required", index) }
		if item.ConnectionCount <= 0 { return Snapshot{}, fmt.Errorf("presence item %d: connectionCount must be greater than zero", index) }
		if item.Source == "" { item.Source = FileCollectorSource }
		if item.Confidence == "" { item.Confidence = "exact" }
		if item.Confidence != "exact" && item.Confidence != "heuristic" { return Snapshot{}, fmt.Errorf("presence item %d: confidence must be exact or heuristic", index) }
	}
	return snapshot, nil
}

