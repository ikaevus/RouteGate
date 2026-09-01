package tasks

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestPrepareReceiptPrecedesRuntimeReadinessProbe(t *testing.T) {
	store := testPlatformUpdateReceiptStore(t)
	taskID := "550e8400-e29b-41d4-a716-446655440000"
	request := PlatformUpdateRequest{SchemaVersion: PlatformUpdateSchemaVersion, TargetVersion: "v1.2.3"}
	probeObservedPrepared := false

	err := preparePlatformUpdateReceiptBeforeReadiness(store, taskID, request, func() bool {
		receipt, readErr := store.Read(taskID)
		if readErr != nil {
			t.Fatalf("prepared receipt was not durable before readiness probe: %v", readErr)
		}
		if receipt.Phase != PlatformUpdateReceiptPrepared || receipt.MutationStarted || receipt.TargetVersion != request.TargetVersion {
			t.Fatalf("unexpected receipt at readiness probe: %+v", receipt)
		}
		probeObservedPrepared = true
		return false
	})
	if err == nil {
		t.Fatal("unsafe runtime readiness unexpectedly succeeded")
	}
	if !probeObservedPrepared {
		t.Fatal("runtime readiness probe ran without a durable prepared receipt")
	}
	receipt, readErr := store.Read(taskID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if receipt.Phase != PlatformUpdateReceiptFailed || receipt.MutationStarted || receipt.Code != "runtime_not_ready" {
		t.Fatalf("runtime readiness failure was not durably terminalized: %+v", receipt)
	}
}

func TestStagePlatformUpdatePersistsPreparedReceiptBeforeStaging(t *testing.T) {
	store := testPlatformUpdateReceiptStore(t)
	taskID := "550e8400-e29b-41d4-a716-446655440000"
	request := PlatformUpdateRequest{SchemaVersion: PlatformUpdateSchemaVersion, TargetVersion: "v1.2.3"}
	stageObservedPrepared := false

	candidate, err := stagePlatformUpdateWithPreparedReceipt(
		context.Background(),
		store,
		taskID,
		request,
		func(_ context.Context, gotTaskID string, gotRequest PlatformUpdateRequest) (PlatformUpdateStagedCandidate, error) {
			receipt, readErr := store.Read(taskID)
			if readErr != nil {
				return PlatformUpdateStagedCandidate{}, fmt.Errorf("prepared receipt was not durable before staging: %w", readErr)
			}
			if receipt.Phase != PlatformUpdateReceiptPrepared || receipt.MutationStarted || receipt.TargetVersion != request.TargetVersion {
				return PlatformUpdateStagedCandidate{}, fmt.Errorf("unexpected pre-staging receipt: %+v", receipt)
			}
			stageObservedPrepared = true
			return PlatformUpdateStagedCandidate{TaskID: gotTaskID, TargetVersion: gotRequest.TargetVersion}, nil
		},
	)
	if err != nil {
		t.Fatalf("stage with prepared receipt: %v", err)
	}
	if !stageObservedPrepared {
		t.Fatal("staging began without observing the durable prepared receipt")
	}
	if candidate.TaskID != taskID || candidate.TargetVersion != request.TargetVersion {
		t.Fatalf("unexpected staged candidate: %+v", candidate)
	}
	receipt, err := store.Read(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Phase != PlatformUpdateReceiptPrepared || receipt.MutationStarted {
		t.Fatalf("successful staging changed prepared handoff unexpectedly: %+v", receipt)
	}
}

func TestStagePlatformUpdateFailureTerminalizesExistingPreparedReceipt(t *testing.T) {
	store := testPlatformUpdateReceiptStore(t)
	taskID := "550e8400-e29b-41d4-a716-446655440000"
	request := PlatformUpdateRequest{SchemaVersion: PlatformUpdateSchemaVersion, TargetVersion: "v1.2.3"}
	stageErr := errors.New("download interrupted")

	_, err := stagePlatformUpdateWithPreparedReceipt(
		context.Background(),
		store,
		taskID,
		request,
		func(context.Context, string, PlatformUpdateRequest) (PlatformUpdateStagedCandidate, error) {
			receipt, readErr := store.Read(taskID)
			if readErr != nil {
				return PlatformUpdateStagedCandidate{}, fmt.Errorf("prepared receipt was not durable before staging failure: %w", readErr)
			}
			if receipt.Phase != PlatformUpdateReceiptPrepared {
				return PlatformUpdateStagedCandidate{}, fmt.Errorf("staging failure did not start from prepared receipt: %+v", receipt)
			}
			return PlatformUpdateStagedCandidate{}, stageErr
		},
	)
	if !errors.Is(err, stageErr) {
		t.Fatalf("stage error=%v want wrapped/preserved %v", err, stageErr)
	}
	receipt, readErr := store.Read(taskID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if receipt.Phase != PlatformUpdateReceiptFailed || receipt.MutationStarted || receipt.Code != "staging_failed" {
		t.Fatalf("staging failure was not durably terminalized before mutation: %+v", receipt)
	}
}

func TestStagePlatformUpdateRefusesExistingReceiptBeforeCallingStager(t *testing.T) {
	store := testPlatformUpdateReceiptStore(t)
	taskID := "550e8400-e29b-41d4-a716-446655440000"
	request := PlatformUpdateRequest{SchemaVersion: PlatformUpdateSchemaVersion, TargetVersion: "v1.2.3"}
	if _, err := store.CreatePrepared(taskID, request.TargetVersion); err != nil {
		t.Fatal(err)
	}
	stageCalled := false

	_, err := stagePlatformUpdateWithPreparedReceipt(
		context.Background(),
		store,
		taskID,
		request,
		func(context.Context, string, PlatformUpdateRequest) (PlatformUpdateStagedCandidate, error) {
			stageCalled = true
			return PlatformUpdateStagedCandidate{}, nil
		},
	)
	if !errors.Is(err, ErrPlatformUpdateDispatchAmbiguous) {
		t.Fatalf("existing receipt did not fail closed to dispatch ambiguity: %v", err)
	}
	if stageCalled {
		t.Fatal("stager ran after durable prior dispatch evidence already existed")
	}
}
