package tasks

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"
)

type RuntimeRecoveryRetentionHistory struct {
	Proof        RuntimeRecoveryRetentionProof
	LocalReceipt RuntimeRecoveryRetentionReceipt
}

// Read only, under the stable original admission flock. The caller must first
// authenticate current credentials/binding. Missing ack is historical uncertainty,
// never permission to write a receipt, resume an owner or remove any barrier.
func WithHistoricalRecoveryRetention(ctx context.Context, dir string, b RuntimeRecoveryGrantBinding, fn func(RuntimeRecoveryRetentionHistory) error) error {
	if ctx.Err() != nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	lock, e := lockRuntimeMutationExisting(dir)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	defer lock.Close()
	pin, e := readTerminalConfirmationFile(filepath.Join(dir, runtimeRecoveryRetentionSubmissionFile), 8192)
	var p RuntimeRecoveryRetentionProof
	if e != nil || !exactHistoricalJSON(pin, &p) {
		return ErrRuntimeMutationBlocked
	}
	var ack []byte
	var receipt RuntimeRecoveryRetentionReceipt
	path := filepath.Join(dir, runtimeRecoveryRetentionAcknowledgementFile)
	if !runtimeMarkerAbsent(path) {
		ack, e = readTerminalConfirmationFile(path, 4096)
		if e != nil || !exactHistoricalJSON(ack, &receipt) || !retentionReceiptMatches(receipt, p, pin) {
			return ErrRuntimeMutationBlocked
		}
	}
	return withHistoricalRecoveryPreparedSubmissionLocked(ctx, dir, b, lock, [][]byte{pin, ack}, func(h RuntimeRecoveryPreparedSubmissionHistory, captured, expiry time.Time) error {
		preparedAck, e := json.Marshal(h.LocalReceipt)
		if e != nil || h.RetentionHash == "" || captured.IsZero() || p.ObservedAt.IsZero() || p.ObservedAt.Before(captured) || !p.ObservedAt.Before(expiry) || p != retentionProofForPrepared(h.Proof, preparedAck, h.RetentionHash, captured, p.ObservedAt) || ctx.Err() != nil {
			return ErrRuntimeMutationBlocked
		}
		return fn(RuntimeRecoveryRetentionHistory{p, receipt})
	})
}
