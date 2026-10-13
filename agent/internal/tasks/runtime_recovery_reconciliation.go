package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

// WithHistoricalRecoverySubmission reads a crashed owner's exact pinned proof
// under the existing admission flock. It never resumes ownership, observes a
// current runtime, writes an acknowledgement, or grants release authority.
// Binding must be independently authenticated with current credentials.
func WithHistoricalRecoverySubmission(ctx context.Context, dir string, binding RuntimeRecoveryGrantBinding, fn func(RuntimeRecoveryCommitProof) error) error {
	if ctx.Err() != nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	lock, err := lockRuntimeMutationExisting(dir)
	if err != nil {
		return ErrRuntimeMutationBlocked
	}
	defer lock.Close()
	return withHistoricalRecoverySubmissionLocked(ctx, dir, binding, lock, func(p RuntimeRecoveryCommitProof, _ func() bool) error {
		if !runtimeRecoveryFinalStateAbsent(dir) {
			return ErrRuntimeMutationBlocked
		}
		err := fn(p)
		if !runtimeRecoveryFinalStateAbsent(dir) {
			return ErrRuntimeMutationBlocked
		}
		return err
	})
}

// Internal reuse for a final consumer holding the same existing flock. The
// snapshot closure is filesystem-only; each caller enforces its own context.
func withHistoricalRecoverySubmissionLocked(ctx context.Context, dir string, binding RuntimeRecoveryGrantBinding, lock *os.File, fn func(RuntimeRecoveryCommitProof, func() bool) error) error {
	if lock == nil || ctx.Err() != nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	if _, err := lock.Stat(); err != nil {
		return ErrRuntimeMutationBlocked
	}
	var err error
	paths := []string{"mutation-inflight.json", runtimeResultFile, runtimeRecoveryConsumptionFile, runtimeRecoverySubmissionFile, runtimeRecoveryAcknowledgementFile}
	limits := []int64{16384, runtimeResultLimit, runtimeRecoveryConsumptionLimit, 8192, 4096}
	data := make([][]byte, len(paths))
	for i, name := range paths {
		path := filepath.Join(dir, name)
		if i == 4 && runtimeMarkerAbsent(path) {
			continue
		}
		data[i], err = readRemovalFile(path, limits[i], true)
		if err != nil {
			return ErrRuntimeMutationBlocked
		}
	}
	unchanged := func() bool {
		if !runtimeMarkerAbsent(filepath.Join(dir, "inflight.json")) {
			return false
		}
		for i, name := range paths {
			path := filepath.Join(dir, name)
			if data[i] == nil {
				if !runtimeMarkerAbsent(path) {
					return false
				}
				continue
			}
			current, e := readRemovalFile(path, limits[i], true)
			if e != nil || !bytes.Equal(current, data[i]) {
				return false
			}
		}
		return true
	}
	var record runtimeRecoveryConsumption
	var proof RuntimeRecoveryCommitProof
	if !exactHistoricalJSON(data[2], &record) || !exactHistoricalJSON(data[3], &proof) {
		return ErrRuntimeMutationBlocked
	}
	grantWire, err := json.Marshal(record.Grant)
	grant, grantErr := decodeRuntimeRecoveryMutationGrant(grantWire)
	if err != nil || grantErr != nil || record.SchemaVersion != 1 || record.Binding != binding || !grant.validBinding(binding, grant.IssuedAt) || record.GrantHash != markerDigest(grantWire) || record.Observation != grant.Expected || record.ObservedAt.Before(grant.IssuedAt) || !record.ObservedAt.Before(grant.ExpiresAt) || proof.SchemaVersion != 1 || proof.OperationID != grant.OperationID || proof.GrantHash != record.GrantHash || proof.ConsumptionHash != markerDigest(data[2]) || proof.Observation != grant.Expected || proof.ObservedAt.Before(record.ObservedAt) || !proof.ObservedAt.Before(grant.ExpiresAt) || grant.Expected.MarkerHash != markerDigest(data[0]) || grant.Expected.ReceiptHash != markerDigest(data[1]) {
		return ErrRuntimeMutationBlocked
	}
	var intent struct {
		SchemaVersion int    `json:"schemaVersion"`
		TaskID        string `json:"taskId"`
		Kind          string `json:"kind"`
	}
	var taskReceipt runtimeResultReceipt
	if !exactHistoricalJSON(data[0], &intent) || intent.SchemaVersion != 1 || intent.TaskID != grant.Expected.TaskID || (intent.Kind != TaskKindConfigApply && intent.Kind != TaskKindVPNCoreService) || !exactHistoricalJSON(data[1], &taskReceipt) || taskReceipt.SchemaVersion != 1 || !taskReceipt.Acknowledged || taskReceipt.Binding != binding.RuntimeResultBinding || taskReceipt.TaskID != intent.TaskID || taskReceipt.MarkerHash != markerDigest(data[0]) || !validRuntimeResult(taskReceipt.Envelope) || markerDigest(taskReceipt.Envelope) != grant.Expected.EnvelopeHash || taskReceipt.RuntimeWitness == nil || *taskReceipt.RuntimeWitness != grant.Expected.Witness {
		return ErrRuntimeMutationBlocked
	}
	if data[4] != nil {
		var receipt RuntimeRecoveryCommitReceipt
		if !exactHistoricalJSON(data[4], &receipt) || receipt.SchemaVersion != 1 || !receipt.Recorded || receipt.OperationID != proof.OperationID || receipt.GrantHash != proof.GrantHash || receipt.ConsumptionHash != proof.ConsumptionHash || receipt.ProofHash != markerDigest(data[3]) || receipt.RequestHash != receipt.ProofHash {
			return ErrRuntimeMutationBlocked
		}
	}
	if ctx.Err() != nil || !unchanged() || fn(proof, unchanged) != nil || ctx.Err() != nil || !unchanged() {
		return ErrRuntimeMutationBlocked
	}
	return nil
}

// Private durable records were written as exact typed JSON. Requiring identical
// bytes rejects duplicates, aliases, unknown/missing fields and trailing data.
func exactHistoricalJSON(data []byte, out any) bool {
	if json.Unmarshal(data, out) != nil {
		return false
	}
	wire, err := json.Marshal(out)
	return err == nil && bytes.Equal(data, wire)
}
