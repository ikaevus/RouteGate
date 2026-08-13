package observability

import (
	"encoding/json"
	"time"
)

// HealthState is the evaluated operational condition of a RouteGate resource.
// It deliberately excludes lifecycle concepts such as maintenance, disabled,
// or acknowledged; those belong to separate dimensions of state.
type HealthState string

const (
	HealthHealthy   HealthState = "healthy"
	HealthDegraded  HealthState = "degraded"
	HealthUnhealthy HealthState = "unhealthy"
	HealthUnknown   HealthState = "unknown"
)

func (s HealthState) Valid() bool {
	switch s {
	case HealthHealthy, HealthDegraded, HealthUnhealthy, HealthUnknown:
		return true
	default:
		return false
	}
}

type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

func (s Severity) Valid() bool {
	switch s {
	case SeverityWarning, SeverityCritical:
		return true
	default:
		return false
	}
}

// AlertState models the condition lifecycle only. Acknowledgement is
// intentionally orthogonal so an alert can remain firing while acknowledged.
type AlertState string

const (
	AlertPending  AlertState = "pending"
	AlertFiring   AlertState = "firing"
	AlertResolved AlertState = "resolved"
)

func (s AlertState) Valid() bool {
	switch s {
	case AlertPending, AlertFiring, AlertResolved:
		return true
	default:
		return false
	}
}

func (s AlertState) Active() bool {
	return s == AlertPending || s == AlertFiring
}

// CanTransitionTo describes one alert episode. A resolved episode never
// reopens; recurrence creates a new episode with the same fingerprint.
func (s AlertState) CanTransitionTo(next AlertState) bool {
	if !s.Valid() || !next.Valid() || s == next {
		return false
	}

	switch s {
	case AlertPending:
		return next == AlertFiring || next == AlertResolved
	case AlertFiring:
		return next == AlertResolved
	default:
		return false
	}
}

// ResourceRef is a provider-neutral reference to an observed RouteGate
// resource. ID is intentionally a string so singleton resources (for example,
// the Manager itself) and UUID-backed resources share one observability model.
type ResourceRef struct {
	Type string
	ID   string
}

// Metric is a numeric runtime observation. High-frequency metric samples are
// not durable PostgreSQL product state; Prometheus-compatible infrastructure is
// responsible for time-series retention when historical metrics are enabled.
type Metric struct {
	Key        string
	Resource   ResourceRef
	Value      float64
	Unit       string
	ObservedAt time.Time
}

// HealthCheck is the current evaluated result for one operational condition.
// ReasonCode and RecommendedAction are machine-readable inputs to Guided
// Workflow / Next Action First UI surfaces.
type HealthCheck struct {
	Key               string
	Resource          ResourceRef
	Component         string
	State             HealthState
	Required          bool
	ReasonCode        string
	Summary           string
	RecommendedAction string
	Evidence          json.RawMessage
	ObservedAt        time.Time
	ExpiresAt         *time.Time
}

// EffectiveState treats expired evidence as Unknown without mutating the
// original evaluated state. A known failure can therefore expire cleanly when
// its source stops reporting.
func (h HealthCheck) EffectiveState(now time.Time) HealthState {
	if h.ExpiresAt != nil && !now.Before(*h.ExpiresAt) {
		return HealthUnknown
	}
	return h.State
}

// Event is a durable, domain-significant operational occurrence. It is not a
// generic log record.
type Event struct {
	ID                   string
	Type                 string
	Source               string
	Resource             ResourceRef
	CorrelationID        string
	PayloadSchemaVersion int
	Payload              json.RawMessage
	OccurredAt           time.Time
	ObservedAt           time.Time
}

// Alert represents one alert episode. Fingerprint identifies the logical
// condition across recurrences, while ID identifies this concrete episode.
type Alert struct {
	ID                     string
	Fingerprint            string
	RuleKey                string
	Resource               ResourceRef
	Severity               Severity
	State                  AlertState
	Summary                string
	ReasonCode             string
	StartedAt              time.Time
	FiringAt               *time.Time
	ResolvedAt             *time.Time
	LastEvaluatedAt        time.Time
	AcknowledgedAt         *time.Time
	AcknowledgedByUserID   string
}

func (a Alert) Active() bool {
	return a.State.Active()
}

func (a Alert) Acknowledged() bool {
	return a.AcknowledgedAt != nil
}

type DiagnosticStatus string

const (
	DiagnosticQueued    DiagnosticStatus = "queued"
	DiagnosticRunning   DiagnosticStatus = "running"
	DiagnosticSucceeded DiagnosticStatus = "succeeded"
	DiagnosticFailed    DiagnosticStatus = "failed"
)

func (s DiagnosticStatus) Valid() bool {
	switch s {
	case DiagnosticQueued, DiagnosticRunning, DiagnosticSucceeded, DiagnosticFailed:
		return true
	default:
		return false
	}
}

// DiagnosticRun is an allow-listed diagnostics operation. ProfileKey resolves
// to registered Agent handlers; it is never an arbitrary shell command.
type DiagnosticRun struct {
	ID          string
	ProfileKey  string
	Resource    ResourceRef
	Status      DiagnosticStatus
	RequestedAt time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

type DiagnosticResult struct {
	CheckKey           string
	State              HealthState
	ReasonCode         string
	Summary            string
	RecommendedAction  string
	Evidence           json.RawMessage
}
