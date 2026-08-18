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

const (
	ApplyJobActionApply = "apply"

	ApplyJobStatusPending    = "pending"
	ApplyJobStatusInProgress = "in_progress"
	ApplyJobStatusSucceeded  = "succeeded"
	ApplyJobStatusFailed     = "failed"
)

const SchemaVersion = "routegate.config.v1"

type ConfigVersion struct {
	ID             string          `json:"id"`
	ServerID       string          `json:"serverId"`
	Version        int             `json:"version"`
	ConfigHash     string          `json:"configHash"`
	Status         string          `json:"status"`
	RenderedConfig json.RawMessage `json:"-"`
	CreatedAt      time.Time       `json:"createdAt"`
	AppliedAt      *time.Time      `json:"appliedAt,omitempty"`
	Pinned         bool            `json:"pinned"`
}

type ConfigApplyJob struct {
	ID              string          `json:"id"`
	ServerID        string          `json:"serverId"`
	AgentID         string          `json:"agentId,omitempty"`
	ConfigVersionID string          `json:"configVersionId"`
	Action          string          `json:"action"`
	Status          string          `json:"status"`
	RequestPayload  json.RawMessage `json:"requestPayload"`
	ResultPayload   json.RawMessage `json:"resultPayload"`
	ErrorMessage    string          `json:"errorMessage,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	StartedAt       *time.Time      `json:"startedAt,omitempty"`
	CompletedAt     *time.Time      `json:"completedAt,omitempty"`
}

type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

type ServerConfigInfo struct {
	ID                string
	Name              string
	DeploymentRole    string
	Hostname          string
	PublicIP          string
	PrivateIP         string
	Location          string
	Provider          string
	Status            string
	VLESSPort         int
	VLESSFlow         string
	VLESSNetwork      string
	RealityPrivateKey string
	RealityPublicKey  string
	RealityShortID    string
	RealityServerName string
	Agent             *AgentConfigInfo
	VPNAccounts       []VPNAccountConfigInfo
	RoutingProfile    *RoutingProfileConfigInfo
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

type VPNAccountConfigInfo struct {
	ID                       string
	DisplayName              string
	Status                   string
	VLESSUUID                string
	VLESSFlow                string
	VLESSNetwork             string
	TrafficEnforcementStatus string
}

type RoutingProfileConfigInfo struct {
	ID          string
	Name        string
	Description string
	IsDefault   bool
	Rules       []RoutingProfileRuleConfigInfo
}

type RoutingProfileRuleConfigInfo struct {
	ID             string
	Name           string
	Priority       int
	Action         string
	Domains        []string
	DomainSuffixes []string
	DomainKeywords []string
	IPCIDRs        []string
	GeoSites       []string
	GeoIPs         []string
}

type RenderedConfig struct {
	SchemaVersion  string                `json:"schemaVersion"`
	Server         ConfigServer          `json:"server"`
	Agent          *ConfigAgent          `json:"agent,omitempty"`
	VPNAccounts    []ConfigVPNAccount    `json:"vpnAccounts"`
	RoutingProfile *ConfigRoutingProfile `json:"routingProfile,omitempty"`
	SingBox        SingBoxConfig         `json:"singBox"`
	Metadata       ConfigMetadata        `json:"metadata"`
}

type ConfigServer struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	DeploymentRole string `json:"deploymentRole"`
	Hostname       string `json:"hostname,omitempty"`
	PublicIP       string `json:"publicIp,omitempty"`
	PrivateIP      string `json:"privateIp,omitempty"`
	Location       string `json:"location,omitempty"`
	Provider       string `json:"provider,omitempty"`
	Status         string `json:"status"`
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

type ConfigVPNAccount struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
	VLESSUUID   string `json:"vlessUuid"`
}

type ConfigRoutingProfile struct {
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	Description string                     `json:"description,omitempty"`
	IsDefault   bool                       `json:"isDefault"`
	Rules       []ConfigRoutingProfileRule `json:"rules"`
}

type ConfigRoutingProfileRule struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Priority       int      `json:"priority"`
	Action         string   `json:"action"`
	Outbound       string   `json:"outbound"`
	Domains        []string `json:"domains,omitempty"`
	DomainSuffixes []string `json:"domainSuffixes,omitempty"`
	DomainKeywords []string `json:"domainKeywords,omitempty"`
	IPCIDRs        []string `json:"ipCidrs,omitempty"`
	GeoSites       []string `json:"geoSites,omitempty"`
	GeoIPs         []string `json:"geoIps,omitempty"`
}

type ConfigMetadata struct {
	Source         string    `json:"source"`
	RenderedAt     time.Time `json:"renderedAt"`
	RealityEnabled bool      `json:"realityEnabled"`
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

type CreateConfigApplyJobInput struct {
	ServerID        string
	AgentID         string
	ConfigVersionID string
	Action          string
	RequestPayload  map[string]any
}
