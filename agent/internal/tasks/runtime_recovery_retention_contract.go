package tasks

import "time"

// Shared Manager/Agent wire contract. A receipt acknowledges storage only.
const RuntimeRecoveryRetentionPurpose = "runtime_mutation_fence_release_retention_evidence_v1"

// An authenticated Agent commitment to its retained private history, not proof
// of filesystem durability, traffic, deletion or authority to release a fence.
type RuntimeRecoveryRetentionProof struct {
	SchemaVersion             int                        `json:"schemaVersion"`
	Purpose                   string                     `json:"purpose"`
	OperationID               string                     `json:"operationId"`
	AgentCredentialGeneration int64                      `json:"agentCredentialGeneration"`
	AuthorizationHash         string                     `json:"authorizationHash"`
	PreparedProofHash         string                     `json:"preparedProofHash"`
	PreparedReceiptHash       string                     `json:"preparedReceiptHash"`
	RetentionHash             string                     `json:"retentionHash"`
	RetainedAt                time.Time                  `json:"retainedAt"`
	ObservedAt                time.Time                  `json:"observedAt"`
	Observation               RuntimeRecoveryObservation `json:"observation"`
}
type RuntimeRecoveryRetentionReceipt struct {
	SchemaVersion int    `json:"schemaVersion"`
	Recorded      bool   `json:"recorded"`
	OperationID   string `json:"operationId"`
	RetentionHash string `json:"retentionHash"`
	ProofHash     string `json:"proofHash"`
	RequestHash   string `json:"requestHash"`
}
