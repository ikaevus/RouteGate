package tasks

import (
	"context"
	"encoding/json"
	"path/filepath"
)

// Historical commitments only. A zero LocalReceipt means the crash left no
// acknowledgement file; a Manager readback never repairs that file or resumes
// the original owner. This value grants no runtime mutation authority.
type RuntimeRecoveryPreparedSubmissionHistory struct {
	Proof        RuntimeRecoveryPreparedReleaseProof
	LocalReceipt RuntimeRecoveryPreparedReleaseReceipt
}

// Read the exact pin, optional acknowledgement and complete retained history
// under the original admission flock. No clock/runtime capture or file writes.
// Current binding must be independently authenticated by the caller first.
func WithHistoricalRecoveryPreparedSubmission(ctx context.Context, dir string, b RuntimeRecoveryGrantBinding, fn func(RuntimeRecoveryPreparedSubmissionHistory) error) error {
	if ctx.Err() != nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	lock, e := lockRuntimeMutationExisting(dir)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	defer lock.Close()
	pin, e := readTerminalConfirmationFile(filepath.Join(dir, runtimeRecoveryPreparedSubmissionFile), 8192)
	var proof RuntimeRecoveryPreparedReleaseProof
	if e != nil || !exactHistoricalJSON(pin, &proof) {
		return ErrRuntimeMutationBlocked
	}
	var ack []byte
	var receipt RuntimeRecoveryPreparedReleaseReceipt
	ackPath := filepath.Join(dir, runtimeRecoveryPreparedAcknowledgementFile)
	if !runtimeMarkerAbsent(ackPath) {
		ack, e = readTerminalConfirmationFile(ackPath, 4096)
		if e != nil || !exactHistoricalJSON(ack, &receipt) || !preparedReleaseReceiptMatches(receipt, proof, pin) {
			return ErrRuntimeMutationBlocked
		}
	}
	return withHistoricalRecoveryPreparedReleaseLocked(ctx, dir, b, lock, [][]byte{pin, ack}, func(h RuntimeRecoveryPreparedReleaseHistory) error {
		terminalWire, e := json.Marshal(h.TerminalProof)
		if e != nil {
			return ErrRuntimeMutationBlocked
		}
		// Terminal receipt bytes were validated by the full historical chain.
		terminalAck, e := readTerminalConfirmationFile(filepath.Join(dir, runtimeRecoveryTerminalAcknowledgementFile), 4096)
		expected := RuntimeRecoveryPreparedReleaseProof{SchemaVersion: 1, Purpose: RuntimeRecoveryPreparedReleasePurpose,
			OperationID: h.TerminalProof.OperationID, AgentCredentialGeneration: b.AgentCredentialGeneration,
			AuthorizationHash: h.TerminalProof.AuthorizationHash, TerminalProofHash: markerDigest(terminalWire),
			TerminalReceiptHash: markerDigest(terminalAck), ReleaseIntentHash: h.ReleaseIntentHash,
			PreparedAt: h.PreparedAt, ObservedAt: proof.ObservedAt, Observation: h.TerminalProof.Observation}
		if e != nil || proof != expected || proof.ObservedAt.IsZero() || proof.ObservedAt.Before(h.PreparedAt) || !proof.ObservedAt.Before(h.AuthorizationExpiresAt) {
			return ErrRuntimeMutationBlocked
		}
		return fn(RuntimeRecoveryPreparedSubmissionHistory{proof, receipt})
	})
}
