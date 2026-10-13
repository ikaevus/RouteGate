package tasks

import (
	"bytes"
	"encoding/json"
	"time"
)

// Conditional transport contract only. No nonce consumption or release method.
type RuntimeRecoveryReleaseAuthorization struct {
	SchemaVersion             int                        `json:"schemaVersion"`
	Purpose                   string                     `json:"purpose"`
	OperationID               string                     `json:"operationId"`
	Nonce                     string                     `json:"nonce"`
	AgentCredentialGeneration int64                      `json:"agentCredentialGeneration"`
	GrantHash                 string                     `json:"grantHash"`
	ConsumptionHash           string                     `json:"consumptionHash"`
	CommitProofHash           string                     `json:"commitProofHash"`
	IssuedAt                  time.Time                  `json:"issuedAt"`
	ExpiresAt                 time.Time                  `json:"expiresAt"`
	Expected                  RuntimeRecoveryObservation `json:"expected"`
}

func ValidateRuntimeRecoveryReleaseAuthorization(data []byte, b RuntimeRecoveryGrantBinding, p RuntimeRecoveryCommitProof, now time.Time) (RuntimeRecoveryReleaseAuthorization, string, error) {
	var a RuntimeRecoveryReleaseAuthorization
	deny := func() (RuntimeRecoveryReleaseAuthorization, string, error) {
		return RuntimeRecoveryReleaseAuthorization{}, "", ErrRuntimeMutationBlocked
	}
	if len(data) > 8192 {
		return deny()
	}
	_, canonical, _, err := canonicalRemovalJSON(data)
	if err != nil {
		return deny()
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil {
		return deny()
	}
	wire, err := json.Marshal(a)
	if err != nil {
		return deny()
	}
	_, typed, _, err := canonicalRemovalJSON(wire)
	proofWire, proofErr := json.Marshal(p)
	o := a.Expected
	if err != nil || proofErr != nil || !bytes.Equal(canonical, typed) || a.SchemaVersion != 1 || a.Purpose != "runtime_mutation_fence_final_release_v1" || !canonicalTaskIDPattern.MatchString(a.OperationID) || !validRemovalHash(a.Nonce) || !validRemovalHash(a.GrantHash) || !validRemovalHash(a.ConsumptionHash) || !validRemovalHash(a.CommitProofHash) || b.ManagerURL == "" || len(b.ManagerURL) > 2048 || b.AgentCredentialGeneration < 1 || a.AgentCredentialGeneration != b.AgentCredentialGeneration || a.OperationID != p.OperationID || a.GrantHash != p.GrantHash || a.ConsumptionHash != p.ConsumptionHash || a.CommitProofHash != markerDigest(proofWire) || p.SchemaVersion != 1 || p.ObservedAt.IsZero() || a.Expected != p.Observation || o.SchemaVersion != 1 || !canonicalTaskIDPattern.MatchString(o.TaskID) || o.AgentID != b.AgentID || o.ServerID != b.ServerID || !canonicalTaskIDPattern.MatchString(o.AgentID) || !canonicalTaskIDPattern.MatchString(o.ServerID) || !validRemovalHash(o.MarkerHash) || !validRemovalHash(o.ReceiptHash) || !validRemovalHash(o.EnvelopeHash) || !validRuntimeWitness(o.Witness) || a.IssuedAt.IsZero() || a.IssuedAt.Before(p.ObservedAt) || !a.ExpiresAt.After(a.IssuedAt) || a.ExpiresAt.Sub(a.IssuedAt) > 2*time.Minute || now.UTC().Before(a.IssuedAt) || !now.UTC().Before(a.ExpiresAt) {
		return deny()
	}
	return a, markerDigest(wire), nil
}
