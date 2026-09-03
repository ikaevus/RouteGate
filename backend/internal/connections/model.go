package connections

import "time"

const (
	MaxSnapshotItems  = 1000
	PresenceTTL       = 75 * time.Second
	RecentActivityTTL = 2 * time.Minute
)

type SnapshotItem struct {
	VPNAccountID  string     `json:"vpnAccountId"`
	Protocol      string     `json:"protocol"`
	ConnectionCount int      `json:"connectionCount"`
	Source        string     `json:"source"`
	Confidence    string     `json:"confidence"`
	ConnectedAt   *time.Time `json:"connectedAt,omitempty"`
	LastActivityAt *time.Time `json:"lastActivityAt,omitempty"`
}

type SnapshotRequest struct {
	ObservedAt time.Time      `json:"observedAt"`
	Items      []SnapshotItem `json:"items"`
}

type SnapshotResponse struct {
	OK       bool   `json:"ok"`
	AgentID  string `json:"agentId"`
	ServerID string `json:"serverId"`
	Accepted int    `json:"accepted"`
}

type Connection struct {
	VPNAccountID   string     `json:"vpnAccountId"`
	AccountName    string     `json:"accountName"`
	Email          string     `json:"email,omitempty"`
	ServerID       string     `json:"serverId"`
	ServerName     string     `json:"serverName"`
	AgentID        string     `json:"agentId,omitempty"`
	AgentName      string     `json:"agentName,omitempty"`
	Protocol       string     `json:"protocol"`
	State          string     `json:"state"`
	ConnectionCount int       `json:"connectionCount"`
	Source         string     `json:"source"`
	Confidence     string     `json:"confidence"`
	ConnectedAt    *time.Time `json:"connectedAt,omitempty"`
	LastActivityAt *time.Time `json:"lastActivityAt,omitempty"`
	ObservedAt     time.Time  `json:"observedAt"`
}

type Summary struct {
	OnlineUsers         int `json:"onlineUsers"`
	OnlineConnections   int `json:"onlineConnections"`
	RecentlyActiveUsers int `json:"recentlyActiveUsers"`
}

type ListResponse struct {
	GeneratedAt time.Time    `json:"generatedAt"`
	Summary     Summary      `json:"summary"`
	Items       []Connection `json:"items"`
}

type SnapshotInput struct {
	ObservedAt time.Time
	Items      []SnapshotItem
}

