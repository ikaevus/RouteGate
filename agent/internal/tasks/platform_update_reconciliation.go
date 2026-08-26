package tasks

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	platformUpdateSystemctlProbePath = "/usr/bin/systemctl"
	platformUpdateWorkerProbeTimeout = 5 * time.Second
	platformUpdateWorkerProbeMaxBytes = 512
)

type PlatformUpdateReconciliationState string

const (
	PlatformUpdateReconciliationPending        PlatformUpdateReconciliationState = "pending"
	PlatformUpdateReconciliationSucceeded      PlatformUpdateReconciliationState = "succeeded"
	PlatformUpdateReconciliationFailed         PlatformUpdateReconciliationState = "failed"
	PlatformUpdateReconciliationOutcomeUnknown PlatformUpdateReconciliationState = "outcome_unknown"
)

type PlatformUpdateReconciliation struct {
	TaskID          string                            `json:"taskId"`
	TargetVersion   string                            `json:"targetVersion"`
	State           PlatformUpdateReconciliationState `json:"state"`
	MutationStarted bool                              `json:"-"`
	Code            string                            `json:"code,omitempty"`
	CreatedAt       time.Time                         `json:"createdAt"`
	UpdatedAt       time.Time                         `json:"updatedAt"`
}

type platformUpdateWorkerProbe func(context.Context, string) (bool, error)

// ReadPlatformUpdateReconciliation is host-mutation read-only. It may advance
// the bounded durable receipt monotonically when the fixed task-specific
// transient unit is proven absent, but it never stages a release, launches a
// worker, invokes the updater, or changes arbitrary host state.
func ReadPlatformUpdateReconciliation(ctx context.Context, taskID, targetVersion string) (PlatformUpdateReconciliation, error) {
	return reconcilePlatformUpdateForManager(ctx, fixedPlatformUpdateReceiptStore(), taskID, targetVersion, fixedPlatformUpdateWorkerActive)
}

func reconcilePlatformUpdateForManager(ctx context.Context, store platformUpdateReceiptStore, taskID, targetVersion string, probe platformUpdateWorkerProbe) (PlatformUpdateReconciliation, error) {
	reconciliation, err := readPlatformUpdateReconciliation(store, taskID, targetVersion)
	if err != nil {
		return PlatformUpdateReconciliation{}, err
	}
	if reconciliation.State != PlatformUpdateReconciliationPending {
		return reconciliation, nil
	}
	if probe == nil {
		return PlatformUpdateReconciliation{}, fmt.Errorf("platform update worker probe is unavailable")
	}

	active, err := probe(ctx, taskID)
	if err != nil {
		return PlatformUpdateReconciliation{}, fmt.Errorf("probe platform update worker: %w", err)
	}
	if active {
		return reconciliation, nil
	}

	var receipt PlatformUpdateReceipt
	if reconciliation.MutationStarted {
		receipt, err = store.MarkOutcomeUnknown(taskID, "detached_worker_missing")
	} else {
		// A prepared receipt plus a definitely absent task-specific unit means no
		// mutation can start from this attempt. A concurrent launcher that has not
		// yet created its unit will subsequently find the receipt terminal and its
		// worker will fail closed before mutation_started.
		receipt, err = store.MarkPreDispatchFailed(taskID, "detached_worker_missing")
	}
	if err != nil {
		return PlatformUpdateReconciliation{}, fmt.Errorf("reconcile absent platform update worker: %w", err)
	}
	return projectPlatformUpdateReceipt(receipt), nil
}

func readPlatformUpdateReconciliation(store platformUpdateReceiptStore, taskID, targetVersion string) (PlatformUpdateReconciliation, error) {
	if !canonicalTaskIDPattern.MatchString(taskID) {
		return PlatformUpdateReconciliation{}, fmt.Errorf("platform update task id must be canonical UUIDv4")
	}
	if !routeGateReleaseVersionPattern.MatchString(targetVersion) {
		return PlatformUpdateReconciliation{}, fmt.Errorf("invalid RouteGate target release version")
	}
	receipt, err := store.Read(taskID)
	if err != nil {
		return PlatformUpdateReconciliation{}, fmt.Errorf("read platform update receipt: %w", err)
	}
	if receipt.TargetVersion != targetVersion {
		return PlatformUpdateReconciliation{}, fmt.Errorf("platform update receipt target version mismatch")
	}
	return projectPlatformUpdateReceipt(receipt), nil
}

func projectPlatformUpdateReceipt(receipt PlatformUpdateReceipt) PlatformUpdateReconciliation {
	state := PlatformUpdateReconciliationPending
	switch receipt.Phase {
	case PlatformUpdateReceiptPrepared, PlatformUpdateReceiptMutationStarted:
		state = PlatformUpdateReconciliationPending
	case PlatformUpdateReceiptSucceeded:
		state = PlatformUpdateReconciliationSucceeded
	case PlatformUpdateReceiptFailed:
		state = PlatformUpdateReconciliationFailed
	case PlatformUpdateReceiptOutcomeUnknown:
		state = PlatformUpdateReconciliationOutcomeUnknown
	}
	return PlatformUpdateReconciliation{
		TaskID:          receipt.TaskID,
		TargetVersion:   receipt.TargetVersion,
		State:           state,
		MutationStarted: receipt.MutationStarted,
		Code:            receipt.Code,
		CreatedAt:       receipt.CreatedAt,
		UpdatedAt:       receipt.UpdatedAt,
	}
}

func fixedPlatformUpdateWorkerActive(parent context.Context, taskID string) (bool, error) {
	if !canonicalTaskIDPattern.MatchString(taskID) {
		return false, fmt.Errorf("platform update task id must be canonical UUIDv4")
	}
	ctx, cancel := context.WithTimeout(parent, platformUpdateWorkerProbeTimeout)
	defer cancel()
	unit := "routegate-vpn-update-" + taskID + ".service"
	cmd := exec.CommandContext(ctx, platformUpdateSystemctlProbePath,
		"show",
		"--no-pager",
		"--property=LoadState",
		"--property=ActiveState",
		unit,
	)
	cmd.Env = platformUpdateWorkerEnvironment
	output, runErr := cmd.Output()
	if len(output) > platformUpdateWorkerProbeMaxBytes {
		return false, fmt.Errorf("platform update worker state output exceeded bounded size")
	}
	active, parseErr := parsePlatformUpdateWorkerState(string(output))
	if parseErr == nil {
		return active, nil
	}
	if runErr != nil {
		return false, fmt.Errorf("query fixed platform update worker unit: %w", runErr)
	}
	return false, parseErr
}

func parsePlatformUpdateWorkerState(output string) (bool, error) {
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] == "LoadState" || parts[0] == "ActiveState" {
			values[parts[0]] = parts[1]
		}
	}
	loadState, ok := values["LoadState"]
	if !ok {
		return false, fmt.Errorf("platform update worker LoadState is missing")
	}
	activeState, ok := values["ActiveState"]
	if !ok {
		return false, fmt.Errorf("platform update worker ActiveState is missing")
	}
	if loadState == "not-found" {
		return false, nil
	}
	if loadState != "loaded" {
		return false, fmt.Errorf("unexpected platform update worker LoadState %q", loadState)
	}
	switch activeState {
	case "active", "activating", "reloading", "deactivating":
		return true, nil
	case "inactive", "failed":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected platform update worker ActiveState %q", activeState)
	}
}
