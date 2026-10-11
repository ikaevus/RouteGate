package revocations

import (
	"bytes"
	"encoding/json"
	"time"
)

// These wire types mirror Agent tasks. Shared golden vectors pin serialization.
// They describe observations only, never an authorization to release a fence.
type RecoveryWitness struct {
	SchemaVersion      int               `json:"schemaVersion"`
	RuntimeHash        string            `json:"runtimeHash"`
	Generation         RuntimeGeneration `json:"generation"`
	PreviousGeneration RuntimeGeneration `json:"previousGeneration"`
	ListenerPort       int               `json:"listenerPort"`
}
type RecoveryObservation struct {
	SchemaVersion int             `json:"schemaVersion"`
	TaskID        string          `json:"taskId"`
	AgentID       string          `json:"agentId"`
	ServerID      string          `json:"serverId"`
	MarkerHash    string          `json:"markerHash"`
	ReceiptHash   string          `json:"receiptHash"`
	EnvelopeHash  string          `json:"envelopeHash"`
	Witness       RecoveryWitness `json:"witness"`
}
type RecoveryChallenge struct {
	SchemaVersion int                 `json:"schemaVersion"`
	OperationID   string              `json:"operationId"`
	Nonce         string              `json:"nonce"`
	IssuedAt      time.Time           `json:"issuedAt"`
	ExpiresAt     time.Time           `json:"expiresAt"`
	Expected      RecoveryObservation `json:"expected"`
}

func decodeRecoveryObservation(data []byte) (RecoveryObservation, error) {
	var o RecoveryObservation
	if len(data) > 8192 {
		return o, ErrPreflight
	}
	if _, _, _, err := canonicalRemovalJSON(data); err != nil {
		return o, ErrPreflight
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&o) != nil {
		return RecoveryObservation{}, ErrPreflight
	}
	for _, id := range []string{o.TaskID, o.AgentID, o.ServerID} {
		if !evidenceID.MatchString(id) {
			return RecoveryObservation{}, ErrPreflight
		}
	}
	for _, h := range []string{o.MarkerHash, o.ReceiptHash, o.EnvelopeHash, o.Witness.RuntimeHash} {
		if !validRemovalHash(h) {
			return RecoveryObservation{}, ErrPreflight
		}
	}
	w := o.Witness
	if o.SchemaVersion != 1 || w.SchemaVersion != 1 || !validGeneration(w.Generation) || !validGeneration(w.PreviousGeneration) || w.Generation.InvocationID == w.PreviousGeneration.InvocationID || w.ListenerPort < 1 || w.ListenerPort > 65535 {
		return RecoveryObservation{}, ErrPreflight
	}
	return o, nil
}
