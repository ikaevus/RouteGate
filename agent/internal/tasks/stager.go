package tasks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type ConfigTask struct {
	ID              string          `json:"id"`
	ServerID        string          `json:"serverId"`
	AgentID         string          `json:"agentId"`
	ConfigVersionID string          `json:"configVersionId"`
	Action          string          `json:"action"`
	Status          string          `json:"status"`
	RenderedConfig  json.RawMessage `json:"renderedConfig"`
	ConfigHash      string          `json:"configHash"`
}

type StageResult struct {
	StagedPath      string `json:"stagedPath"`
	ConfigHash      string `json:"configHash"`
	ConfigVersionID string `json:"configVersionId"`
}

type Stager struct {
	dir string
}

func NewStager(dir string) Stager {
	return Stager{dir: dir}
}

func (s Stager) Stage(task ConfigTask) (StageResult, error) {
	if task.ID == "" {
		return StageResult{}, fmt.Errorf("task id is required")
	}
	if task.ConfigVersionID == "" {
		return StageResult{}, fmt.Errorf("config version id is required")
	}
	if !json.Valid(task.RenderedConfig) {
		return StageResult{}, fmt.Errorf("rendered config must be valid JSON")
	}
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return StageResult{}, fmt.Errorf("create config staging dir: %w", err)
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, task.RenderedConfig, "", "  "); err != nil {
		return StageResult{}, fmt.Errorf("format rendered config: %w", err)
	}
	pretty.WriteByte('\n')

	path := filepath.Join(s.dir, task.ConfigVersionID+".json")
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, pretty.Bytes(), 0o600); err != nil {
		return StageResult{}, fmt.Errorf("write staged config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return StageResult{}, fmt.Errorf("commit staged config: %w", err)
	}

	return StageResult{StagedPath: path, ConfigHash: task.ConfigHash, ConfigVersionID: task.ConfigVersionID}, nil
}
