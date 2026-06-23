package configs

import (
	"encoding/json"
	"time"
)

const (
	StatusRendered         = "rendered"
	StatusValidationFailed = "validation_failed"
	StatusValidated        = "validated"
	StatusStaged           = "staged"
	StatusApplyInProgress  = "apply_in_progress"
	StatusApplied          = "applied"
	StatusCommitConfirmed  = "commit_confirmed"
	StatusRollbackProgress = "rollback_in_progress"
	StatusRolledBack       = "rolled_back"
	StatusFailed           = "failed"
)

const SchemaVersion = "routegate.config.v1"

type ConfigVersion struct {
	ID             string          `json:"id"`
	ServerID       string          `json:"serverId"`
	Version        int             `json:"version"`
	ConfigHash     string          `json:"configHash"`
	Status         string          `json:"status"`
	RenderedConfig json.RawMessage `json:"renderedConfig"`
	CreatedAt      time.Time       `json:"createdAt"`
	AppliedAt      *time.Time      `json:"appliedAt,omitempty"`
}

type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

type ServerConfigInfo struct {
	ID          string
	Name        string
	Hostname    string
	PublicIP    string
	PrivateIP   string
	Location    string
	Provider    string
	Status      string
	Agent       *AgentConfigInfo
}

type AgentConfigInfo struct {
	ID           string
	Hostname     string
	OS           string
	Arch         string
	AgentVersion string
	Status       string
	Capabilities map[string]any
}

type RenderedConfig struct {
	SchemaVersion string         `json:"schemaVersion"`
	Server        ConfigServer   `json:"server"`
	Agent         *ConfigAgent   `json:"agent,omitempty"`
	SingBox       SingBoxConfig  `json:"singBox"`
	Metadata      ConfigMetadata `json:"metadata"`
}

type ConfigServer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Hostname  string `json:"hostname,omitempty"`
	PublicIP  string `json:"publicIp,omitempty"`
	PrivateIP string `json:"privateIp,omitempty"`
	Location  string `json:"location,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Status    string `json:"status"`
}

type ConfigAgent struct {
	ID           string         `json:"id"`
	Hostname     string         `json:"hostname,omitempty"`
	OS           string         `json:"os,omitempty"`
	Arch         string         `json:"arch,omitempty"`
	AgentVersion string         `json:"agentVersion"`
	Status       string         `json:"status"`
	Capabilities map[string]any `json:"capabilities"`
}

type ConfigMetadata struct {
	Source     string    `json:"source"`
	RenderedAt time.Time `json:"renderedAt"`
}

type SingBoxConfig struct {
	Log       SingBoxLog        `json:"log"`
	Inbounds  []map[string]any  `json:"inbounds"`
	Outbounds []SingBoxOutbound `json:"outbounds"`
	Route     SingBoxRoute      `json:"route"`
}

type SingBoxLog struct {
	Level string `json:"level"`
}

type SingBoxOutbound struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
}

type SingBoxRoute struct {
	Rules []map[string]any `json:"rules"`
	Final string           `json:"final"`
}

type CreateConfigVersionInput struct {
	ServerID       string
	Status         string
	ConfigHash     string
	RenderedConfig RenderedConfig
}
