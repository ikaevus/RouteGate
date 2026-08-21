package nodegroups

import "time"

const (
	SelectionStrategyPriority = "priority"
	SelectionStrategyWeighted = "weighted"

	CandidateHealthReady       = "ready"
	CandidateHealthDegraded    = "degraded"
	CandidateHealthUnavailable = "unavailable"
)

type NodeGroup struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Description       string            `json:"description,omitempty"`
	SelectionStrategy string            `json:"selectionStrategy"`
	MemberCount       int               `json:"memberCount"`
	Members           []NodeGroupMember `json:"members,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}

type NodeGroupMember struct {
	NodeGroupID   string    `json:"nodeGroupId"`
	ServerID      string    `json:"serverId"`
	ServerName    string    `json:"serverName"`
	Protocol      string    `json:"protocol"`
	DeploymentRole string   `json:"deploymentRole"`
	Priority      int       `json:"priority"`
	Weight        int       `json:"weight"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type NodeGroupCandidate struct {
	ServerID       string     `json:"serverId"`
	ServerName     string     `json:"serverName"`
	Protocol       string     `json:"protocol"`
	Priority       int        `json:"priority"`
	Weight         int        `json:"weight"`
	MemberEnabled  bool       `json:"memberEnabled"`
	NodeStatus     string     `json:"nodeStatus"`
	AgentStatus    string     `json:"agentStatus,omitempty"`
	LastSeenAt     *time.Time `json:"lastSeenAt,omitempty"`
	Load1          *float64   `json:"load1,omitempty"`
	LogicalCPUs    *int       `json:"logicalCpus,omitempty"`
	LoadPerCPU     *float64   `json:"loadPerCpu,omitempty"`
	ProtocolSupported bool    `json:"protocolSupported"`
	RuntimeState      string  `json:"runtimeState,omitempty"`
	Eligible       bool       `json:"eligible"`
	Health         string     `json:"health"`
	Signals        []string   `json:"signals"`
}

type ListNodeGroupsResponse struct {
	Items []NodeGroup `json:"items"`
}

type ListNodeGroupCandidatesResponse struct {
	NodeGroupID       string               `json:"nodeGroupId"`
	SelectionStrategy string               `json:"selectionStrategy"`
	Candidates        []NodeGroupCandidate `json:"candidates"`
}

type CreateNodeGroupRequest struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	SelectionStrategy string `json:"selectionStrategy"`
}

type UpdateNodeGroupRequest struct {
	Name              *string `json:"name,omitempty"`
	Description       *string `json:"description,omitempty"`
	SelectionStrategy *string `json:"selectionStrategy,omitempty"`
}

type UpsertNodeGroupMemberRequest struct {
	Priority *int  `json:"priority,omitempty"`
	Weight   *int  `json:"weight,omitempty"`
	Enabled  *bool `json:"enabled,omitempty"`
}

type CreateNodeGroupInput struct {
	Name              string
	Description       string
	SelectionStrategy string
}

type UpdateNodeGroupInput struct {
	Name              *string
	Description       *string
	SelectionStrategy *string
}

type UpsertNodeGroupMemberInput struct {
	NodeGroupID string
	ServerID    string
	Priority    int
	Weight      int
	Enabled     bool
}

func validSelectionStrategy(value string) bool {
	return value == SelectionStrategyPriority || value == SelectionStrategyWeighted
}
