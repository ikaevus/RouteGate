package revocations

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const RecoveryAdmissionReleasePurpose = "runtime_mutation_admission_release_evidence_v1"

// Contract only. Neither decoding nor predecessor matching accepts storage,
// authenticates an Agent, or completes a Manager reservation.
type RecoveryAdmissionReleaseProof struct {
	SchemaVersion             int                 `json:"schemaVersion"`
	Purpose                   string              `json:"purpose"`
	OperationID               string              `json:"operationId"`
	AgentCredentialGeneration int64               `json:"agentCredentialGeneration"`
	AuthorizationHash         string              `json:"authorizationHash"`
	CleanedProofHash          string              `json:"cleanedProofHash"`
	CleanedReceiptHash        string              `json:"cleanedReceiptHash"`
	CleanedArchiveHash        string              `json:"cleanedArchiveHash"`
	ReleasedAt                time.Time           `json:"releasedAt"`
	ObservedAt                time.Time           `json:"observedAt"`
	Observation               RecoveryObservation `json:"observation"`
}

func DecodeRecoveryAdmissionReleaseProof(data []byte) (RecoveryAdmissionReleaseProof, error) {
	var zero, p RecoveryAdmissionReleaseProof
	if len(data) > 8192 {
		return zero, ErrPreflight
	}
	_, canonical, _, e := canonicalRemovalJSON(data)
	if e != nil {
		return zero, ErrPreflight
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return zero, ErrPreflight
	}
	wire, e := json.Marshal(p)
	if e != nil {
		return zero, ErrPreflight
	}
	_, typed, _, e := canonicalRemovalJSON(wire)
	if e != nil || !bytes.Equal(canonical, typed) || p.SchemaVersion != 1 || p.Purpose != RecoveryAdmissionReleasePurpose || !evidenceID.MatchString(p.OperationID) || p.AgentCredentialGeneration < 1 || p.ReleasedAt.IsZero() || p.ObservedAt.IsZero() || p.ObservedAt.Before(p.ReleasedAt) {
		return zero, ErrPreflight
	}
	for _, h := range []string{p.AuthorizationHash, p.CleanedProofHash, p.CleanedReceiptHash, p.CleanedArchiveHash} {
		if !validRemovalHash(h) {
			return zero, ErrPreflight
		}
	}
	o, _ := json.Marshal(p.Observation)
	if _, e := decodeRecoveryObservation(o); e != nil {
		return zero, ErrPreflight
	}
	return p, nil
}

func admissionReleaseMatchesCleaned(p RecoveryAdmissionReleaseProof, data, hash string) bool {
	c, e := DecodeRecoveryCleanedCommitProof([]byte(data))
	sum := sha256.Sum256([]byte(data))
	if e != nil || hex.EncodeToString(sum[:]) != hash || p.CleanedProofHash != hash || p.OperationID != c.OperationID || p.AgentCredentialGeneration != c.AgentCredentialGeneration || p.AuthorizationHash != c.AuthorizationHash || p.Observation != c.Observation || p.ReleasedAt.Before(c.ObservedAt) || p.ObservedAt.Before(p.ReleasedAt) {
		return false
	}
	r, _ := json.Marshal(recoveryCleanedCommitReceipt(c, []byte(data), []byte(data)))
	sum = sha256.Sum256(r)
	return p.CleanedReceiptHash == hex.EncodeToString(sum[:])
}
