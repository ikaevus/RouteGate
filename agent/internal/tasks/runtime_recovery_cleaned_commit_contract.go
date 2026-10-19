package tasks

import "time"

// Shared public wire only. Stored Agent statement, not filesystem evidence,
// traffic proof or release authority. No delivery/dispatcher caller is added.
const RuntimeRecoveryCleanedCommitPurpose = "runtime_mutation_cleaned_commit_evidence_v1"

type RuntimeRecoveryCleanedCommitProof struct {
	SchemaVersion             int                        `json:"schemaVersion"`
	Purpose                   string                     `json:"purpose"`
	OperationID               string                     `json:"operationId"`
	AgentCredentialGeneration int64                      `json:"agentCredentialGeneration"`
	AuthorizationHash         string                     `json:"authorizationHash"`
	RetentionProofHash        string                     `json:"retentionProofHash"`
	RetentionReceiptHash      string                     `json:"retentionReceiptHash"`
	ReleaseIntentHash         string                     `json:"releaseIntentHash"`
	CleanedIntentHash         string                     `json:"cleanedIntentHash"`
	ManifestHash              string                     `json:"manifestHash"`
	MarkerHash                string                     `json:"markerHash"`
	IntentAt                  time.Time                  `json:"intentAt"`
	ObservedAt                time.Time                  `json:"observedAt"`
	Observation               RuntimeRecoveryObservation `json:"observation"`
}

type RuntimeRecoveryCleanedCommitReceipt struct {
	SchemaVersion     int    `json:"schemaVersion"`
	Recorded          bool   `json:"recorded"`
	OperationID       string `json:"operationId"`
	CleanedIntentHash string `json:"cleanedIntentHash"`
	ProofHash         string `json:"proofHash"`
	RequestHash       string `json:"requestHash"`
}
