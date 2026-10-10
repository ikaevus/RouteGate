package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recoveryChallengeFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeResultBinding, RuntimeRecoveryChallenge) {
	t.Helper()
	e, task, adapter, binding := recoveryFixture(t)
	observation, err := ObserveRuntimeRecovery(context.Background(), e.receiptDir, binding, task.ID, e.activePath, adapter)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	return e, adapter, binding, RuntimeRecoveryChallenge{SchemaVersion: 1, OperationID: resultTaskID, Nonce: strings.Repeat("a", 64), IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), Expected: observation}
}

func TestRuntimeMutationRecoveryChallengeReadOnly(t *testing.T) {
	e, a, binding, challenge := recoveryChallengeFixture(t)
	paths := []string{e.activePath, filepath.Join(e.receiptDir, "mutation-inflight.json"), filepath.Join(e.receiptDir, runtimeResultFile)}
	before := make([][]byte, len(paths))
	for i, path := range paths {
		var err error
		before[i], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	var previousHash string
	for i := 0; i < 3; i++ {
		// Read-only retries are intentionally allowed; this is NOT nonce consumption.
		response, err := ObserveRuntimeRecoveryChallenge(context.Background(), e.receiptDir, binding, challenge, e.activePath, a)
		encoded, marshalErr := json.Marshal(challenge)
		if err != nil || marshalErr != nil || response.SchemaVersion != 1 || response.OperationID != challenge.OperationID || response.ChallengeHash != markerDigest(encoded) || response.Observation != challenge.Expected || response.ObservedAt.Before(challenge.IssuedAt) || !response.ObservedAt.Before(challenge.ExpiresAt) {
			t.Fatal("incorrect challenge response", response, err, marshalErr)
		}
		if i == 1 && response.ChallengeHash != previousHash {
			t.Fatal("identical retry changed request binding")
		}
		if i == 2 && response.ChallengeHash == previousHash {
			t.Fatal("nonce did not bind response")
		}
		previousHash = response.ChallengeHash
		if i == 1 {
			challenge.Nonce = strings.Repeat("b", 64)
		}
		output, _ := json.Marshal(response)
		for _, secret := range []string{removalUUID, "DO-NOT-LOG", e.activePath, binding.ManagerURL} {
			if bytes.Contains(output, []byte(secret)) {
				t.Fatal("secret/path in response")
			}
		}
	}
	for i, path := range paths {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before[i], after) {
			t.Fatal("challenge changed durable state", err)
		}
	}
	if lease, err := BeginRuntimeMutation(e.receiptDir, challenge.Expected.TaskID, TaskKindVPNCoreService); err == nil {
		lease.Close()
		t.Fatal("challenge unlocked mutation")
	}
	if a.restarts != 1 || a.validations != 0 {
		t.Fatal("challenge mutated runtime")
	}
}

func TestRuntimeMutationRecoveryChallengeRejectsMismatch(t *testing.T) {
	for _, mode := range []string{"schema", "operation", "nonce", "future", "expired", "lifetime", "zero_time", "observation_schema", "task", "agent", "server", "manager", "marker", "receipt", "envelope", "runtime", "generation", "previous_generation", "port", "cancelled", "expired_during_health", "clock_backwards", "concurrent"} {
		t.Run(mode, func(t *testing.T) {
			e, a, binding, c := recoveryChallengeFixture(t)
			ctx := context.Background()
			now := time.Now().UTC()
			clock := func() time.Time { return now }
			switch mode {
			case "schema":
				c.SchemaVersion++
			case "operation":
				c.OperationID = "not-an-id"
			case "nonce":
				c.Nonce = strings.Repeat("a", 63)
			case "future":
				c.IssuedAt = now.Add(time.Second)
			case "expired":
				c.ExpiresAt = now
			case "lifetime":
				c.ExpiresAt = c.IssuedAt.Add(runtimeRecoveryChallengeLifetime + time.Nanosecond)
			case "zero_time":
				c.IssuedAt = time.Time{}
			case "observation_schema":
				c.Expected.SchemaVersion++
			case "task":
				c.Expected.TaskID = resultTaskID
			case "agent":
				c.Expected.AgentID = resultTaskID
			case "server":
				c.Expected.ServerID = resultTaskID
			case "manager":
				binding.ManagerURL = "https://other.invalid"
			case "marker":
				c.Expected.MarkerHash = strings.Repeat("f", 64)
			case "receipt":
				c.Expected.ReceiptHash = strings.Repeat("f", 64)
			case "envelope":
				c.Expected.EnvelopeHash = strings.Repeat("f", 64)
			case "runtime":
				c.Expected.Witness.RuntimeHash = strings.Repeat("f", 64)
			case "generation":
				c.Expected.Witness.Generation.PID++
			case "previous_generation":
				c.Expected.Witness.PreviousGeneration.PID++
			case "port":
				c.Expected.Witness.ListenerPort++
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "expired_during_health":
				a.healthHook = func() error { now = c.ExpiresAt; return nil }
			case "clock_backwards":
				a.healthHook = func() error { now = now.Add(-time.Millisecond); return nil }
			case "concurrent":
				lock, err := lockRuntimeMutation(e.receiptDir)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
			}
			response, err := observeRuntimeRecoveryChallenge(ctx, e.receiptDir, binding, c, e.activePath, a, clock)
			if !errors.Is(err, ErrRuntimeMutationBlocked) || response != (RuntimeRecoveryChallengeResponse{}) {
				t.Fatal("unsafe challenge accepted", response, err)
			}
			if runtimeMarkerAbsent(filepath.Join(e.receiptDir, "mutation-inflight.json")) {
				t.Fatal("failure removed fence")
			}
		})
	}
}
