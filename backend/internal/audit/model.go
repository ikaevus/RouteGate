package audit

import "time"

const (
	ActorTypeUser      = "user"
	ActorTypeAgent     = "agent"
	ActorTypeSystem    = "system"
	ActorTypeAnonymous = "anonymous"

	ResultSuccess = "success"
	ResultFailure = "failure"
)

type Event struct {
	ID           string
	ActorUserID  string
	ActorType    string
	Action       string
	ResourceType string
	ResourceID   string
	Result       string
	Metadata     map[string]any
	CreatedAt    time.Time
}

type EventInput struct {
	ActorUserID  string
	ActorType    string
	Action       string
	ResourceType string
	ResourceID   string
	Result       string
	Metadata     map[string]any
}
