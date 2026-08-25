package updates

import (
	"encoding/json"
	"time"
)

const (
	OperationPreflight = "preflight"
	OperationDiscovery = "discovery"
	OperationStage     = "stage"
	OperationApply     = "apply"
	StagePreflight     = "preflight"
	StageDiscovery     = "discovery"
	StageStage         = "stage"
	StageApply         = "apply"

	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"

	DecisionProceed = "proceed"
	DecisionBlocked = "blocked"

	AvailabilityUpdateAvailable     = "update_available"
	AvailabilityUpToDate            = "up_to_date"
	AvailabilityCurrentNewer        = "current_newer"
	AvailabilityUnknownCurrent      = "unknown_current_version"
	AvailabilityUncomparableRelease = "uncomparable_release"
	AvailabilityNoRelease           = "no_release"
	AvailabilityUnsupportedPlatform = "unsupported_platform"
	AvailabilityIncompleteRelease   = "incomplete_release"

	DiscoverySourceOfficialGitHub = "github:ikaevus/RouteGate/releases/latest"
	ProvenanceUnverified          = "unverified"
	ProvenanceVerified            = "verified"
	ProvenanceVerificationRG96B   = "rg96b_provenance_and_manifest_verification_required"
	VerificationRG96C3A           = "rg96c3a_non_mutating_verify"

	HostTrustPreflightDeferred = "deferred_to_privileged_b2"
)

type Job struct {
	ID              string          `json:"id"`
	Operation       string          `json:"operation"`
	Status          string          `json:"status"`
	Stage           string          `json:"stage"`
	RequestPayload  json.RawMessage `json:"requestPayload"`
	ResultPayload   json.RawMessage `json:"resultPayload"`
	ErrorCode       string          `json:"errorCode,omitempty"`
	CreatedByUserID string          `json:"createdByUserId,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	StartedAt       *time.Time      `json:"startedAt,omitempty"`
	CompletedAt     *time.Time      `json:"completedAt,omitempty"`
}

type PreflightResult struct {
	Decision                  string   `json:"decision"`
	Blockers                  []string `json:"blockers"`
	ManagerVersion            string   `json:"managerVersion"`
	ManagerGitCommit          string   `json:"managerGitCommit"`
	ManagerBuildDate          string   `json:"managerBuildDate"`
	DatabaseAppliedMigration  string   `json:"databaseAppliedMigration"`
	ExpectedSchemaVersion     int      `json:"expectedSchemaVersion"`
	UpdateStatus              string   `json:"updateStatus"`
	UpdateChannel             string   `json:"updateChannel"`
	AutomaticUpdatesSupported bool     `json:"automaticUpdatesSupported"`
	HostTrustPreflight        string   `json:"hostTrustPreflight"`
}

type DiscoveryAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type DiscoveryResult struct {
	Source               string           `json:"source"`
	CurrentVersion       string           `json:"currentVersion"`
	CandidateVersion     string           `json:"candidateVersion,omitempty"`
	PublishedAt          string           `json:"publishedAt,omitempty"`
	RuntimeOS            string           `json:"runtimeOs"`
	RuntimeArch          string           `json:"runtimeArch"`
	Assets               []DiscoveryAsset `json:"assets"`
	MissingAssets        []string         `json:"missingAssets"`
	Availability         string           `json:"availability"`
	ProvenanceStatus     string           `json:"provenanceStatus"`
	VerificationRequired string           `json:"verificationRequired"`
}

type StageRequest struct {
	DiscoveryJobID string `json:"discoveryJobId"`
}

type VerifiedArtifact struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type StageResult struct {
	DiscoveryJobID    string           `json:"discoveryJobId"`
	CandidateVersion  string           `json:"candidateVersion"`
	VerifiedVersion   string           `json:"verifiedVersion"`
	VerifiedCommit    string           `json:"verifiedCommit"`
	ExpectedMigration string           `json:"expectedMigration"`
	RuntimeOS         string           `json:"runtimeOs"`
	RuntimeArch       string           `json:"runtimeArch"`
	Artifact          VerifiedArtifact `json:"artifact"`
	ProvenanceStatus  string           `json:"provenanceStatus"`
	Verification      string           `json:"verification"`
}

type ApplyRequest struct {
	StageJobID string `json:"stageJobId"`
}

type ApplyResult struct {
	StageJobID        string           `json:"stageJobId"`
	CandidateVersion  string           `json:"candidateVersion"`
	VerifiedVersion   string           `json:"verifiedVersion"`
	VerifiedCommit    string           `json:"verifiedCommit"`
	ExpectedMigration string           `json:"expectedMigration"`
	RuntimeOS         string           `json:"runtimeOs"`
	RuntimeArch       string           `json:"runtimeArch"`
	Artifact          VerifiedArtifact `json:"artifact"`
	ProvenanceStatus  string           `json:"provenanceStatus"`
	Verification      string           `json:"verification"`
}

type ListResponse struct {
	Items []Job `json:"items"`
}

type CreateResponse struct {
	Job Job `json:"job"`
}
