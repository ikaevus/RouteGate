package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Historical commitments only. A zero LocalReceipt means the crash left no
// acknowledgement file; a Manager readback never repairs that file or resumes
// the original owner. This value grants no runtime mutation authority.
type RuntimeRecoveryPreparedSubmissionHistory struct {
	Proof         RuntimeRecoveryPreparedReleaseProof
	LocalReceipt  RuntimeRecoveryPreparedReleaseReceipt
	RetentionHash string
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
	return withHistoricalRecoveryPreparedSubmissionLocked(ctx, dir, b, lock, nil, func(h RuntimeRecoveryPreparedSubmissionHistory, _, _ time.Time) error { return fn(h) })
}

// Only the dedicated retention reader supplies the final proof/receipt pair.
// Older public readers keep their exact pre-retention-submission inventory.
func withHistoricalRecoveryPreparedSubmissionLocked(ctx context.Context, dir string, b RuntimeRecoveryGrantBinding, lock *os.File, extra [][]byte, fn func(RuntimeRecoveryPreparedSubmissionHistory, time.Time, time.Time) error) error {
	if len(extra) != 0 && len(extra) != 2 {
		return ErrRuntimeMutationBlocked
	}
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
	confirmations := [][]byte{pin, ack}
	var retention []byte
	retentionPath := filepath.Join(dir, runtimeRecoveryReleaseRetentionFile)
	if !runtimeMarkerAbsent(retentionPath) {
		retention, e = readTerminalConfirmationFile(retentionPath, runtimeRecoveryReleaseRetentionLimit)
		if e != nil || ack == nil {
			return ErrRuntimeMutationBlocked
		}
		confirmations = append(confirmations, retention)
	}
	if len(extra) != 0 {
		if retention == nil || extra[0] == nil {
			return ErrRuntimeMutationBlocked
		}
		confirmations = append(confirmations, extra...)
	}
	return withHistoricalRecoveryPreparedReleaseLocked(ctx, dir, b, lock, confirmations, func(h RuntimeRecoveryPreparedReleaseHistory) error {
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
		out := RuntimeRecoveryPreparedSubmissionHistory{Proof: proof, LocalReceipt: receipt}
		var unchanged func() bool
		var captured time.Time
		if retention != nil {
			archive, e := retirementArchivePath(dir, proof.OperationID)
			if e != nil {
				return ErrRuntimeMutationBlocked
			}
			unchanged, e = validateReleaseHistoryRetention(retention, dir, archive, b, proof, h.AuthorizationExpiresAt)
			if e != nil {
				return ErrRuntimeMutationBlocked
			}
			out.RetentionHash = markerDigest(retention)
			var retained runtimeRecoveryReleaseRetention
			if !exactHistoricalJSON(retention, &retained) {
				return ErrRuntimeMutationBlocked
			}
			captured = retained.RetainedAt
		}
		if fn(out, captured, h.AuthorizationExpiresAt) != nil || (unchanged != nil && !unchanged()) {
			return ErrRuntimeMutationBlocked
		}
		return nil
	})
}
