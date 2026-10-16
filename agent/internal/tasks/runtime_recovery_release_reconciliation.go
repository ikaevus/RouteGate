package tasks

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// Local prepared-history commitment plus the original pinned terminal proof.
// The Manager only records the latter. Neither this value nor an expired
// authorization is authority to resume, remove barriers or assert live traffic.
type RuntimeRecoveryPreparedReleaseHistory struct {
	ReleaseIntentHash      string
	PreparedAt             time.Time
	AuthorizationExpiresAt time.Time
	TerminalProof          RuntimeRecoveryTerminalProof
}

// Current binding must be independently authenticated first. The original root
// flock and all snapshots span the callback (including historical HTTP readback).
// Read only: no runtime capture, clock renewal, new owner, receipt or cleanup.
func WithHistoricalRecoveryPreparedRelease(ctx context.Context, dir string, b RuntimeRecoveryGrantBinding, fn func(RuntimeRecoveryPreparedReleaseHistory) error) error {
	if ctx.Err() != nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	lock, err := lockRuntimeMutationExisting(dir)
	if err != nil {
		return ErrRuntimeMutationBlocked
	}
	defer lock.Close()
	data, err := readTerminalConfirmationFile(filepath.Join(dir, runtimeRecoveryReleaseIntentFile), 8192)
	var r runtimeRecoveryReleaseIntent
	if err != nil || !exactHistoricalJSON(data, &r) || r.SchemaVersion != 1 || r.Purpose != runtimeRecoveryReleaseIntentPurpose || r.Binding != b || !canonicalTaskIDPattern.MatchString(r.OperationID) || r.PreparedAt.IsZero() {
		return ErrRuntimeMutationBlocked
	}
	archive, err := retirementArchivePath(dir, r.OperationID)
	if err != nil {
		return ErrRuntimeMutationBlocked
	}
	inventory := fenceReleaseInventory(dir, archive, lock, data)
	before, root, arch, err := inventory()
	if err != nil {
		return ErrRuntimeMutationBlocked
	}
	return withPinnedTerminalRecoveryHistoryLocked(ctx, dir, b, lock, data, func(_ RuntimeRecoveryFinalizationProof, p *RuntimeRecoveryTerminalProof) error {
		if p == nil || p.OperationID != r.OperationID {
			return ErrRuntimeMutationBlocked
		}
		consumption, e := readRemovalFile(filepath.Join(archive, runtimeRecoveryFinalConsumptionFile), runtimeRecoveryConsumptionLimit, true)
		var final runtimeRecoveryFinalConsumption
		if e != nil || !exactHistoricalJSON(consumption, &final) {
			return ErrRuntimeMutationBlocked
		}
		plan := RuntimeRecoveryFenceReleasePlan{OperationID: p.OperationID, AuthorizationHash: p.AuthorizationHash,
			TerminalProofHash: before[2].file.Hash, TerminalReceiptHash: before[3].file.Hash,
			Binding: b, ExpiresAt: final.Authorization.ExpiresAt, ArchiveName: filepath.Base(archive)}
		for i := range plan.Files {
			plan.Files[i] = before[i].file
		}
		if r != releaseIntentForPlan(plan, r.PreparedAt) || r.PreparedAt.Before(p.ObservedAt) || !r.PreparedAt.Before(plan.ExpiresAt) || ctx.Err() != nil {
			return ErrRuntimeMutationBlocked
		}
		h := RuntimeRecoveryPreparedReleaseHistory{markerDigest(data), r.PreparedAt, plan.ExpiresAt, *p}
		if fn(h) != nil || ctx.Err() != nil {
			return ErrRuntimeMutationBlocked
		}
		after, rootAfter, archAfter, e := inventory()
		if e != nil || !os.SameFile(root, rootAfter) || !os.SameFile(arch, archAfter) {
			return ErrRuntimeMutationBlocked
		}
		for i := range before {
			if before[i].info == nil && after[i].info == nil {
				continue
			}
			if before[i].file != after[i].file || !os.SameFile(before[i].info, after[i].info) {
				return ErrRuntimeMutationBlocked
			}
		}
		return nil
	})
}
