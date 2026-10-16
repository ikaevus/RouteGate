package tasks

import "time"

// Shared Manager/Agent wire contract. A receipt acknowledges storage only.
const RuntimeRecoveryPreparedReleasePurpose = "runtime_mutation_fence_release_preparation_evidence_v1"

// An authenticated Agent commitment to its prepared release intent, not proof
// of filesystem durability, traffic, deletion or authority to release a fence.
type RuntimeRecoveryPreparedReleaseProof struct {
	SchemaVersion             int                        `json:"schemaVersion"`
	Purpose                   string                     `json:"purpose"`
	OperationID               string                     `json:"operationId"`
	AgentCredentialGeneration int64                      `json:"agentCredentialGeneration"`
	AuthorizationHash         string                     `json:"authorizationHash"`
	TerminalProofHash         string                     `json:"terminalProofHash"`
	TerminalReceiptHash       string                     `json:"terminalReceiptHash"`
	ReleaseIntentHash         string                     `json:"releaseIntentHash"`
	PreparedAt                time.Time                  `json:"preparedAt"`
	ObservedAt                time.Time                  `json:"observedAt"`
	Observation               RuntimeRecoveryObservation `json:"observation"`
}
type RuntimeRecoveryPreparedReleaseReceipt struct {
	SchemaVersion     int    `json:"schemaVersion"`
	Recorded          bool   `json:"recorded"`
	OperationID       string `json:"operationId"`
	ReleaseIntentHash string `json:"releaseIntentHash"`
	ProofHash         string `json:"proofHash"`
	RequestHash       string `json:"requestHash"`
}
