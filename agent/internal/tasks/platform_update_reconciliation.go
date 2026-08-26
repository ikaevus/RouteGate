package tasks

import (
	"fmt"
	"time"
)

type PlatformUpdateReconciliationState string

const (
	PlatformUpdateReconciliationPending PlatformUpdateReconciliationState = "pending"
	PlatformUpdateReconciliationSucceeded PlatformUpdateReconciliationState = "succeeded"
	PlatformUpdateReconciliationFailed PlatformUpdateReconciliationState = "failed"
	PlatformUpdateReconciliationOutcomeUnknown PlatformUpdateReconciliationState = "outcome_unknown"
)

type PlatformUpdateReconciliation struct {
	TaskID string `json:"taskId"`
	TargetVersion string `json:"targetVersion"`
	State PlatformUpdateReconciliationState `json:"state"`
	Code string `json:"code,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func ReadPlatformUpdateReconciliation(taskID, targetVersion string) (PlatformUpdateReconciliation, error) {
	return readPlatformUpdateReconciliation(fixedPlatformUpdateReceiptStore(), taskID, targetVersion)
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
	default:
		return PlatformUpdateReconciliation{}, fmt.Errorf("unsupported platform update receipt phase")
	}
	return PlatformUpdateReconciliation{TaskID: receipt.TaskID, TargetVersion: receipt.TargetVersion, State: state, Code: receipt.Code, CreatedAt: receipt.CreatedAt, UpdatedAt: receipt.UpdatedAt}, nil
}
