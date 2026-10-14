package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
)

// WithHistoricalRecoveryFinalization reads exact crashed final-owner history
// under the existing admission flock. It neither resumes ownership nor observes
// current runtime, writes acknowledgement, consumes authority or releases state.
// Binding must be independently authenticated with current credentials.
func WithHistoricalRecoveryFinalization(ctx context.Context, dir string, binding RuntimeRecoveryGrantBinding, fn func(RuntimeRecoveryFinalizationProof) error) error {
	if ctx.Err() != nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	lock, err := lockRuntimeMutationExisting(dir)
	if err != nil {
		return ErrRuntimeMutationBlocked
	}
	defer lock.Close()
	return withHistoricalRecoverySubmissionLocked(ctx, dir, binding, lock, func(original RuntimeRecoveryCommitProof, _ func() bool) error {
		paths := []string{runtimeRecoveryFinalConsumptionFile, runtimeRecoveryFinalSubmissionFile, runtimeRecoveryFinalAcknowledgementFile}
		limits := []int64{runtimeRecoveryConsumptionLimit, 8192, 4096}
		data := make([][]byte, len(paths))
		for i, name := range paths {
			path := filepath.Join(dir, name)
			if i == 2 && runtimeMarkerAbsent(path) {
				continue
			}
			var e error
			data[i], e = readRemovalFile(path, limits[i], true)
			if e != nil {
				return ErrRuntimeMutationBlocked
			}
		}
		unchanged := func() bool {
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
		var record runtimeRecoveryFinalConsumption
		var proof RuntimeRecoveryFinalizationProof
		if !exactHistoricalJSON(data[0], &record) || !exactHistoricalJSON(data[1], &proof) {
			return ErrRuntimeMutationBlocked
		}
		wire, e := json.Marshal(record.Authorization)
		// Validate the historical authority at its original issuedAt, never now:
		// expired authority is not renewed and cannot resume a live owner.
		a, hash, authErr := ValidateRuntimeRecoveryReleaseAuthorization(wire, binding, original, record.Authorization.IssuedAt)
		if e != nil || authErr != nil || record.SchemaVersion != 1 || record.Binding != binding || record.AuthorizationHash != hash || record.Observation != a.Expected || record.ObservedAt.Before(a.IssuedAt) || !record.ObservedAt.Before(a.ExpiresAt) || proof.SchemaVersion != 1 || proof.OperationID != a.OperationID || proof.AuthorizationHash != hash || proof.FinalConsumptionHash != markerDigest(data[0]) || proof.Observation != a.Expected || proof.ObservedAt.Before(record.ObservedAt) || !proof.ObservedAt.Before(a.ExpiresAt) {
			return ErrRuntimeMutationBlocked
		}
		old, e := readRemovalFile(filepath.Join(dir, runtimeRecoveryConsumptionFile), runtimeRecoveryConsumptionLimit, true)
		var consumed runtimeRecoveryConsumption
		if e != nil || !exactHistoricalJSON(old, &consumed) || a.Nonce == consumed.Grant.Nonce {
			return ErrRuntimeMutationBlocked
		}
		if data[2] != nil {
			var receipt RuntimeRecoveryFinalizationReceipt
			if !exactHistoricalJSON(data[2], &receipt) || receipt.SchemaVersion != 1 || !receipt.Recorded || receipt.OperationID != proof.OperationID || receipt.AuthorizationHash != proof.AuthorizationHash || receipt.FinalConsumptionHash != proof.FinalConsumptionHash || receipt.ProofHash != markerDigest(data[1]) || receipt.RequestHash != receipt.ProofHash {
				return ErrRuntimeMutationBlocked
			}
		}
		if ctx.Err() != nil || !unchanged() || fn(proof) != nil || ctx.Err() != nil || !unchanged() {
			return ErrRuntimeMutationBlocked
		}
		return nil
	})
}
