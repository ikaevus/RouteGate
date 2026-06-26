package agents

import "time"

type ListAgentsResponse struct {
	Items []Agent `json:"items"`
}

type AgentRegistrationRequest struct {
	RegistrationToken string       `json:"registrationToken"`
	Hostname          string       `json:"hostname"`
	AgentVersion      string       `json:"agentVersion"`
	OS                string       `json:"os"`
	Arch              string       `json:"arch"`
	Capabilities      Capabilities `json:"capabilities"`
}

type AgentRegistrationResponse struct {
	AgentID           string `json:"agentId"`
	ServerID          string `json:"serverId"`
	AgentToken        string `json:"agentToken"`
	AgentTokenPreview string `json:"agentTokenPreview"`
}

type AgentHeartbeatRequest struct {
	AgentVersion *string      `json:"agentVersion,omitempty"`
	Capabilities Capabilities `json:"capabilities,omitempty"`
}

type AgentHeartbeatResponse struct {
	OK           bool   `json:"ok"`
	AgentID      string `json:"agentId"`
	ServerID     string `json:"serverId"`
	ServerStatus string `json:"serverStatus"`
}

type AgentNextTaskResponse struct {
	Task *AgentConfigTask `json:"task,omitempty"`
}

type CompleteAgentTaskRequest struct {
	Status        string         `json:"status"`
	ErrorMessage  string         `json:"errorMessage,omitempty"`
	ResultPayload map[string]any `json:"resultPayload,omitempty"`
}

type CompleteAgentTaskResponse struct {
	OK     bool   `json:"ok"`
	TaskID string `json:"taskId"`
	Status string `json:"status"`
}

// RegisterAgentRequest and the legacy heartbeat DTOs are retained until the
// older service layer is removed.
type RegisterAgentRequest struct {
	ServerID string `json:"serverId"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Hostname string `json:"hostname"`
}

type RegisterAgentResponse struct {
	Agent Agent  `json:"agent"`
	Token string `json:"token"`
}

type HeartbeatRequest struct {
	AgentID  string `json:"agentId"`
	Version  string `json:"version"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
}

type HeartbeatResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}
