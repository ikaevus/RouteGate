package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func finalAuthorizationForProof(p RuntimeRecoveryCommitProof, generation int64) RuntimeRecoveryReleaseAuthorization {
	wire, _ := json.Marshal(p)
	now := time.Now().UTC()
	return RuntimeRecoveryReleaseAuthorization{1, "runtime_mutation_fence_final_release_v1", p.OperationID, strings.Repeat("6", 64), generation, p.GrantHash, p.ConsumptionHash, markerDigest(wire), now, now.Add(time.Minute), p.Observation}
}
func finalizationFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, RuntimeRecoveryReleaseAuthorization) {
	t.Helper()
	e, a, b, _, wire := recoveryCommitFixture(t)
	m, err := BeginRuntimeRecoveryCommit(context.Background(), e.receiptDir, b, wire, e.activePath, a)
	if err != nil {
		t.Fatal(err)
	}
	var p RuntimeRecoveryCommitProof
	err = m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryCommitGuard) error {
		var e error
		p, e = g.PinProof(context.Background())
		return e
	})
	m.Close()
	if err != nil {
		t.Fatal(err)
	}
	return e, a, b, finalAuthorizationForProof(p, b.AgentCredentialGeneration)
}

func TestRuntimeMutationFinalizationRetainsState(t *testing.T) {
	e, a, b, auth := finalizationFixture(t)
	wire, _ := json.Marshal(auth)
	entries, _ := os.ReadDir(e.receiptDir)
	before := map[string][]byte{}
	for _, entry := range entries {
		before[entry.Name()], _ = os.ReadFile(filepath.Join(e.receiptDir, entry.Name()))
	}
	m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for range 2 {
		p, e := m.Proof(context.Background())
		if e != nil || p.FinalConsumptionHash == "" || p.AuthorizationHash != markerDigest(wire) || p.Observation != auth.Expected {
			t.Fatal("final measured proof", e)
		}
		encoded, _ := json.Marshal(p)
		for _, secret := range []string{auth.Nonce, b.ManagerURL, removalUUID, m.activePath} {
			if bytes.Contains(encoded, []byte(secret)) {
				t.Fatal("sensitive final proof")
			}
		}
	}
	if lock, e := lockRuntimeMutation(m.dir); e == nil {
		lock.Close()
		t.Fatal("lost exclusive lock")
	}
	for name, want := range before {
		got, e := os.ReadFile(filepath.Join(m.dir, name))
		if e != nil || !bytes.Equal(got, want) {
			t.Fatal("changed historical state", name)
		}
	}
	if a.restarts != 1 || a.validations != 0 {
		t.Fatal("final consumer mutated runtime")
	}
	record, err := readRemovalFile(filepath.Join(m.dir, runtimeRecoveryFinalConsumptionFile), runtimeRecoveryConsumptionLimit, true)
	if err != nil {
		t.Fatal(err)
	}
	var r runtimeRecoveryFinalConsumption
	if !exactHistoricalJSON(record, &r) || r.Authorization != auth || r.Binding != b || r.AuthorizationHash != markerDigest(wire) || r.Observation != auth.Expected {
		t.Fatal("final durable binding")
	}
	m.Close()
	if _, e := m.Proof(context.Background()); e == nil {
		t.Fatal("closed owner observed")
	}
	if _, e := BeginRuntimeRecoveryFinalization(context.Background(), m.dir, b, wire, m.activePath, a); e == nil {
		t.Fatal("nonce resumed")
	}
	if WithHistoricalRecoverySubmission(context.Background(), m.dir, b, func(RuntimeRecoveryCommitProof) error { t.Error("consumed final state delivered"); return nil }) == nil {
		t.Fatal("history adopted final state")
	}
	if lease, e := BeginRuntimeMutation(m.dir, auth.Expected.TaskID, TaskKindVPNCoreService); e == nil {
		lease.Close()
		t.Fatal("final consumption unlocked fence")
	}
}

func TestRuntimeMutationFinalizationRejects(t *testing.T) {
	for _, mode := range []string{"generation", "manager", "nonce_reuse", "grant", "consumption", "proof", "expired", "future", "cancelled", "runtime", "process", "missing_pin", "existing", "publication_drift", "backwards", "expiry_during_observation"} {
		t.Run(mode, func(t *testing.T) {
			e, a, b, auth := finalizationFixture(t)
			ctx := context.Background()
			clock := time.Now().UTC()
			now := func() time.Time { return clock }
			auth.IssuedAt = clock
			auth.ExpiresAt = clock.Add(time.Minute)
			switch mode {
			case "generation":
				b.AgentCredentialGeneration++
			case "manager":
				b.ManagerURL = "https://foreign.invalid"
			case "nonce_reuse":
				data, _ := os.ReadFile(filepath.Join(e.receiptDir, runtimeRecoveryConsumptionFile))
				var r runtimeRecoveryConsumption
				json.Unmarshal(data, &r)
				auth.Nonce = r.Grant.Nonce
			case "grant":
				auth.GrantHash = strings.Repeat("e", 64)
			case "consumption":
				auth.ConsumptionHash = strings.Repeat("e", 64)
			case "proof":
				auth.CommitProofHash = strings.Repeat("e", 64)
			case "expired":
				auth.ExpiresAt = clock
			case "future":
				auth.IssuedAt = clock.Add(time.Second)
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "runtime":
				os.WriteFile(e.activePath, []byte(`{}`), 0600)
			case "process":
				a.generation.InvocationID = strings.Repeat("9", 32)
			case "missing_pin":
				os.Remove(filepath.Join(e.receiptDir, runtimeRecoverySubmissionFile))
			case "existing":
				os.WriteFile(filepath.Join(e.receiptDir, runtimeRecoveryFinalConsumptionFile), []byte(`{}`), 0600)
			case "publication_drift":
				a.healthHook = func() error {
					if !runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryFinalConsumptionFile)) {
						os.WriteFile(e.activePath, []byte(`{}`), 0600)
					}
					return nil
				}
			case "backwards":
				a.healthHook = func() error { clock = clock.Add(-time.Second); return nil }
			case "expiry_during_observation":
				a.healthHook = func() error { clock = auth.ExpiresAt; return nil }
			}
			wire, _ := json.Marshal(auth)
			m, err := beginRuntimeRecoveryFinalization(ctx, e.receiptDir, b, wire, e.activePath, a, now)
			if err != ErrRuntimeMutationBlocked || m != nil {
				if m != nil {
					m.Close()
				}
				t.Fatal("unsafe final consumption", err)
			}
			if mode == "publication_drift" && runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryFinalConsumptionFile)) {
				t.Fatal("publication uncertainty erased consumption")
			}
			if mode != "publication_drift" && mode != "existing" && !runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryFinalConsumptionFile)) {
				t.Fatal("preflight wrote consumption")
			}
		})
	}
}

func TestRuntimeMutationFinalizationPoisoned(t *testing.T) {
	for _, mode := range []string{"tampered", "deleted", "runtime", "cancelled", "backwards", "expired"} {
		t.Run(mode, func(t *testing.T) {
			e, a, b, auth := finalizationFixture(t)
			wire, _ := json.Marshal(auth)
			m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			ctx := context.Background()
			switch mode {
			case "tampered":
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryFinalConsumptionFile), []byte(`{}`), 0600)
			case "deleted":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryFinalConsumptionFile))
			case "runtime":
				a.generation.InvocationID = strings.Repeat("9", 32)
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "backwards":
				m.now = func() time.Time { return m.lastChecked.Add(-time.Second) }
			case "expired":
				m.now = func() time.Time { return auth.ExpiresAt }
			}
			if _, err = m.Proof(ctx); err == nil {
				t.Fatal("drift accepted")
			}
			m.now = time.Now
			if _, err = m.Proof(context.Background()); err == nil {
				t.Fatal("poison revived")
			}
		})
	}
}

func TestRuntimeMutationFinalizationConcurrent(t *testing.T) {
	e, a, b, auth := finalizationFixture(t)
	wire, _ := json.Marshal(auth)
	type result struct {
		owner *RuntimeRecoveryFinalization
		err   error
	}
	ch := make(chan result, 2)
	for range 2 {
		go func() {
			m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
			ch <- result{m, err}
		}()
	}
	x, y := <-ch, <-ch
	success := 0
	for _, r := range []result{x, y} {
		if r.err == nil {
			success++
			r.owner.Close()
		} else if r.err != ErrRuntimeMutationBlocked {
			t.Fatal(r.err)
		}
	}
	if success != 1 {
		t.Fatal("final nonce not exclusive", success)
	}
}

func TestRuntimeMutationFinalizationCloseWaits(t *testing.T) {
	e, a, b, auth := finalizationFixture(t)
	wire, _ := json.Marshal(auth)
	m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	first := true
	a.healthHook = func() error {
		if first {
			first = false
			close(entered)
			<-release
		}
		return nil
	}
	proofDone := make(chan error, 1)
	go func() { _, e := m.Proof(context.Background()); proofDone <- e }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("observation did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	select {
	case <-closed:
		close(release)
		t.Fatal("Close interrupted fresh observation")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err = <-proofDone; err != nil {
		t.Fatal(err)
	}
	if err = <-closed; err != nil {
		t.Fatal(err)
	}
	if runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryFinalConsumptionFile)) {
		t.Fatal("Close erased final consumption")
	}
}

func TestRuntimeMutationFinalizationCrash(t *testing.T) {
	for _, kill := range []bool{false, true} {
		t.Run(map[bool]string{false: "exit", true: "SIGKILL"}[kill], func(t *testing.T) {
			e, _, b, g, _ := recoveryCommitFixture(t)
			input, _ := json.Marshal(struct {
				Dir, Active string
				Binding     RuntimeRecoveryGrantBinding
				Grant       RuntimeRecoveryMutationGrant
			}{e.receiptDir, e.activePath, b, g})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRuntimeMutationRecoveryCommitProcessExit$")
			cmd.Env = append(os.Environ(), "RG_TEST_RECOVERY_COMMIT_CHILD="+string(input), "RG_TEST_RECOVERY_FINAL_CHILD=1")
			if kill {
				cmd.Env = append(cmd.Env, "RG_TEST_RECOVERY_COMMIT_KILL=1")
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() { cmd.Process.Kill(); cmd.Wait() }()
				for runtimeMarkerAbsent(filepath.Join(e.receiptDir, "test-commit-ready")) {
					if ctx.Err() != nil {
						t.Fatal("child failed to consume final authority")
					}
					time.Sleep(10 * time.Millisecond)
				}
				cmd.Process.Kill()
				cmd.Wait()
			} else {
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatal("child failed", err, string(out))
				}
			}
			for _, name := range []string{"mutation-inflight.json", runtimeResultFile, runtimeRecoveryConsumptionFile, runtimeRecoverySubmissionFile, runtimeRecoveryAcknowledgementFile, runtimeRecoveryFinalConsumptionFile} {
				if runtimeMarkerAbsent(filepath.Join(e.receiptDir, name)) {
					t.Fatal("crash lost state", name)
				}
			}
			if lease, err := BeginRuntimeMutation(e.receiptDir, g.Expected.TaskID, TaskKindVPNCoreService); err == nil {
				lease.Close()
				t.Fatal("crash unlocked fence")
			}
		})
	}
}

func TestRuntimeMutationFinalConsumptionStopsLiveCompletion(t *testing.T) {
	e, task, _, _ := removalExecutorFixture(t)
	lease, err := BeginRuntimeMutation(e.receiptDir, task.ID, TaskKindVPNCoreService)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err = os.WriteFile(filepath.Join(e.receiptDir, runtimeRecoveryFinalConsumptionFile), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	b := RuntimeResultBinding{ManagerURL: "https://isolated.invalid", AgentID: task.AgentID, ServerID: task.ServerID}
	if lease.SaveResult(b, task.ID, []byte(`{"status":"succeeded"}`)) == nil {
		t.Fatal("result saved past final consumption")
	}
	if lease.Complete() == nil {
		t.Fatal("live completion removed final fence")
	}
	if runtimeMarkerAbsent(filepath.Join(e.receiptDir, "mutation-inflight.json")) {
		t.Fatal("intent disappeared")
	}
}
