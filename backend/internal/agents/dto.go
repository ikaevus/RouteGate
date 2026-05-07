package agents

import "time"

type ListAgentsResponse struct {
	Items []Agent `json:"items"`
}

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
