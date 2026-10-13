package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recoveryCommitFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, RuntimeRecoveryMutationGrant, []byte) {
	t.Helper()
	e, a, b, c := recoveryChallengeFixture(t)
	bound := RuntimeRecoveryGrantBinding{RuntimeResultBinding: b, AgentCredentialGeneration: 7}
	g := RuntimeRecoveryMutationGrant{1, runtimeRecoveryGrantPurpose, c.OperationID, strings.Repeat("d", 64), 7, strings.Repeat("e", 64), strings.Repeat("f", 64), c.IssuedAt, c.ExpiresAt, c.Expected}
	wire, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	return e, a, bound, g, wire
}

func TestRuntimeMutationRecoveryCommitGolden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-grant-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &v) != nil {
		t.Fatal("invalid fixture")
	}
	g, err := decodeRuntimeRecoveryMutationGrant([]byte(v.Wire))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(g)
	if err != nil || string(wire) != v.Wire || markerDigest(wire) != v.SHA256 {
		t.Fatal("Agent grant serialization drift")
	}
	for _, bad := range []string{`{"schemaVersion":1,` + v.Wire[1:], strings.Replace(v.Wire, `"schemaVersion":1`, `"SchemaVersion":1`, 1), `{"untrusted":"secret",` + v.Wire[1:], v.Wire + `{}`, strings.Repeat(" ", 8192) + v.Wire} {
		if _, err = decodeRuntimeRecoveryMutationGrant([]byte(bad)); err == nil {
			t.Fatal("malformed grant accepted")
		}
	}
}

func TestRuntimeMutationRecoveryCommitHoldsLockAndRetainsFence(t *testing.T) {
	e, a, b, g, wire := recoveryCommitFixture(t)
	paths := []string{e.activePath, filepath.Join(e.receiptDir, "mutation-inflight.json"), filepath.Join(e.receiptDir, runtimeResultFile)}
	before := make([][]byte, len(paths))
	for i, p := range paths {
		var err error
		before[i], err = os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
	}
	m, err := BeginRuntimeRecoveryCommit(context.Background(), e.receiptDir, b, wire, e.activePath, a)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	file := filepath.Join(e.receiptDir, runtimeRecoveryConsumptionFile)
	stored, err := readRemovalFile(file, runtimeRecoveryConsumptionLimit, true)
	if err != nil {
		t.Fatal(err)
	}
	var record runtimeRecoveryConsumption
	if json.Unmarshal(stored, &record) != nil || record.Grant != g || record.Binding != b || record.GrantHash != markerDigest(wire) || record.Observation != g.Expected {
		t.Fatal("consumption not durably bound")
	}
	for range 2 {
		proof, err := m.Proof(context.Background())
		if err != nil || proof.GrantHash != markerDigest(wire) || proof.ConsumptionHash != markerDigest(stored) || proof.Observation != g.Expected || proof.ObservedAt.Before(g.IssuedAt) || !proof.ObservedAt.Before(g.ExpiresAt) {
			t.Fatal("incorrect live-owner proof", err)
		}
		encoded, _ := json.Marshal(proof)
		for _, secret := range []string{g.Nonce, removalUUID, "DO-NOT-LOG", b.ManagerURL, e.activePath} {
			if bytes.Contains(encoded, []byte(secret)) {
				t.Fatal("sensitive proof")
			}
		}
	}
	if l, err := lockRuntimeMutation(e.receiptDir); err == nil {
		l.Close()
		t.Fatal("commit did not retain exclusive lock")
	}
	if ReplayRuntimeResult(e.receiptDir, b.RuntimeResultBinding, func(string, []byte) error { t.Error("receipt replay escaped commit lock"); return nil }) == nil {
		t.Fatal("replay ran during commit")
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Proof(context.Background()); !errors.Is(err, ErrRuntimeMutationBlocked) {
		t.Fatal("closed owner produced proof", err)
	}
	if next, err := BeginRuntimeRecoveryCommit(context.Background(), e.receiptDir, b, wire, e.activePath, a); err == nil {
		next.Close()
		t.Fatal("consumed nonce replayed")
	}
	for i, p := range paths {
		after, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(after, before[i]) {
			t.Fatal("commit altered runtime/receipt/fence", err)
		}
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(after, stored) {
		t.Fatal("proof/retry changed consumption", err)
	}
	if a.restarts != 1 || a.validations != 0 {
		t.Fatal("commit invoked runtime mutation")
	}
}

func TestRuntimeMutationRecoveryCommitRejects(t *testing.T) {
	for _, mode := range []string{"purpose", "nonce", "schema", "generation", "agent", "server", "manager", "zero_generation", "future", "expired", "lifetime", "task", "marker", "receipt", "envelope", "runtime", "process", "previous", "port", "cancelled", "backwards", "expires_during_health", "concurrent"} {
		t.Run(mode, func(t *testing.T) {
			e, a, b, g, _ := recoveryCommitFixture(t)
			ctx := context.Background()
			now := time.Now().UTC()
			clock := func() time.Time { return now }
			switch mode {
			case "purpose":
				g.Purpose = "credential_removal_v1"
			case "nonce":
				g.Nonce = "bad"
			case "schema":
				g.SchemaVersion++
			case "generation":
				b.AgentCredentialGeneration++
			case "zero_generation":
				b.AgentCredentialGeneration = 0
			case "agent":
				b.AgentID = resultTaskID
			case "server":
				b.ServerID = resultTaskID
			case "manager":
				b.ManagerURL = "https://other.invalid"
			case "future":
				g.IssuedAt = now.Add(time.Second)
			case "expired":
				g.ExpiresAt = now
			case "lifetime":
				g.ExpiresAt = g.IssuedAt.Add(3 * time.Minute)
			case "task":
				g.Expected.TaskID = resultTaskID
			case "marker":
				g.Expected.MarkerHash = strings.Repeat("0", 64)
			case "receipt":
				g.Expected.ReceiptHash = strings.Repeat("0", 64)
			case "envelope":
				g.Expected.EnvelopeHash = strings.Repeat("0", 64)
			case "runtime":
				g.Expected.Witness.RuntimeHash = strings.Repeat("0", 64)
			case "process":
				g.Expected.Witness.Generation.PID++
			case "previous":
				g.Expected.Witness.PreviousGeneration.PID++
			case "port":
				g.Expected.Witness.ListenerPort++
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "backwards":
				a.healthHook = func() error { now = now.Add(-time.Second); return nil }
			case "expires_during_health":
				a.healthHook = func() error { now = g.ExpiresAt; return nil }
			case "concurrent":
				l, err := lockRuntimeMutation(e.receiptDir)
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
			}
			wire, _ := json.Marshal(g)
			m, err := beginRuntimeRecoveryCommit(ctx, e.receiptDir, b, wire, e.activePath, a, clock)
			if !errors.Is(err, ErrRuntimeMutationBlocked) || m != nil {
				if m != nil {
					m.Close()
				}
				t.Fatal("unsafe grant admitted", err)
			}
			if !runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryConsumptionFile)) {
				t.Fatal("pre-consumption rejection wrote journal")
			}
			if runtimeMarkerAbsent(filepath.Join(e.receiptDir, "mutation-inflight.json")) {
				t.Fatal("rejection removed fence")
			}
		})
	}
}

func TestRuntimeMutationRecoveryCommitPoisonedOwner(t *testing.T) {
	for _, mode := range []string{"process", "config", "receipt", "consumption", "cancelled", "expired", "backwards", "post_persist"} {
		t.Run(mode, func(t *testing.T) {
			e, a, b, g, wire := recoveryCommitFixture(t)
			now := time.Now().UTC()
			clock := func() time.Time { return now }
			if mode == "post_persist" {
				calls := 0
				a.healthHook = func() error {
					calls++
					if calls == 2 {
						a.generation.PID++
					}
					return nil
				}
				if m, err := beginRuntimeRecoveryCommit(context.Background(), e.receiptDir, b, wire, e.activePath, a, clock); err == nil {
					m.Close()
					t.Fatal("drift after durable consumption accepted")
				}
				if runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryConsumptionFile)) {
					t.Fatal("post-persist error discarded consumption")
				}
				return
			}
			m, err := beginRuntimeRecoveryCommit(context.Background(), e.receiptDir, b, wire, e.activePath, a, clock)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			ctx := context.Background()
			switch mode {
			case "process":
				a.generation.PID++
			case "config":
				err = os.WriteFile(e.activePath, []byte(`{}`), 0600)
			case "receipt":
				err = os.WriteFile(filepath.Join(e.receiptDir, runtimeResultFile), []byte(`{}`), 0600)
			case "consumption":
				err = os.WriteFile(filepath.Join(e.receiptDir, runtimeRecoveryConsumptionFile), []byte(`{}`), 0600)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "expired":
				now = g.ExpiresAt
			case "backwards":
				now = now.Add(-time.Second)
			}
			if err != nil {
				t.Fatal(err)
			}
			if p, err := m.Proof(ctx); !errors.Is(err, ErrRuntimeMutationBlocked) || p.SchemaVersion != 0 {
				t.Fatal("drift accepted", err)
			}
			if _, err := m.Proof(context.Background()); err == nil {
				t.Fatal("poisoned owner recovered automatically")
			}
			if runtimeMarkerAbsent(filepath.Join(e.receiptDir, "mutation-inflight.json")) || runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryConsumptionFile)) {
				t.Fatal("error erased durable state")
			}
		})
	}
}

func TestRuntimeMutationRecoveryConsumptionAloneGuardsMutations(t *testing.T) {
	for _, name := range []string{runtimeRecoveryConsumptionFile, runtimeRecoverySubmissionFile, runtimeRecoveryAcknowledgementFile, runtimeRecoveryFinalConsumptionFile, runtimeRecoveryFinalSubmissionFile, runtimeRecoveryFinalAcknowledgementFile} {
		for _, contents := range []string{"{}", "", "malformed"} {
			t.Run(name+contents, func(t *testing.T) {
				e, task, a, _ := removalExecutorFixture(t)
				l, err := lockRuntimeMutation(e.receiptDir)
				if err != nil {
					t.Fatal(err)
				}
				l.Close()
				if err = os.WriteFile(filepath.Join(e.receiptDir, name), []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
				if lease, err := BeginRuntimeMutation(e.receiptDir, task.ID, TaskKindVPNCoreService); err == nil {
					lease.Close()
					t.Fatal("consumption-only state admitted mutation")
				}
				if _, err = e.Execute(context.Background(), task); err == nil || a.restarts != 0 {
					t.Fatal("consumption-only state admitted removal")
				}
				if ReplayRuntimeResult(e.receiptDir, RuntimeResultBinding{}, func(string, []byte) error { t.Error("unexpected delivery"); return nil }) == nil {
					t.Fatal("consumption-only state allowed replay")
				}
			})
		}
	}
}

func TestRuntimeMutationRecoveryCommitConcurrent(t *testing.T) {
	e, a, b, _, wire := recoveryCommitFixture(t)
	type result struct {
		m   *RuntimeRecoveryCommit
		err error
	}
	ch := make(chan result, 2)
	for range 2 {
		go func() {
			m, err := BeginRuntimeRecoveryCommit(context.Background(), e.receiptDir, b, wire, e.activePath, a)
			ch <- result{m, err}
		}()
	}
	x, y := <-ch, <-ch
	success := 0
	for _, r := range []result{x, y} {
		if r.err == nil {
			success++
			r.m.Close()
		} else if !errors.Is(r.err, ErrRuntimeMutationBlocked) {
			t.Fatal(r.err)
		}
	}
	if success != 1 {
		t.Fatal("concurrent consumption not exclusive", success)
	}
}

func TestRuntimeMutationRecoveryCommitProcessExit(t *testing.T) {
	if input := os.Getenv("RG_TEST_RECOVERY_COMMIT_CHILD"); input != "" {
		var in struct {
			Dir, Active string
			Binding     RuntimeRecoveryGrantBinding
			Grant       RuntimeRecoveryMutationGrant
		}
		if json.Unmarshal([]byte(input), &in) != nil {
			os.Exit(2)
		}
		wire, _ := json.Marshal(in.Grant)
		adapter := &removalAdapter{generation: in.Grant.Expected.Witness.Generation}
		m, err := BeginRuntimeRecoveryCommit(context.Background(), in.Dir, in.Binding, wire, in.Active, adapter)
		if err != nil {
			os.Exit(3)
		}
		if m.WithSubmission(context.Background(), in.Binding, markerDigest(wire), func(g *RuntimeRecoveryCommitGuard) error {
			p, e := g.PinProof(context.Background())
			if e != nil {
				return e
			}
			return g.RecordReceipt(context.Background(), receiptForRecoveryProof(p))
		}) != nil {
			os.Exit(5)
		}
		if os.Getenv("RG_TEST_RECOVERY_FINAL_CHILD") == "1" {
			m.Close()
			data, e := os.ReadFile(filepath.Join(in.Dir, runtimeRecoverySubmissionFile))
			var p RuntimeRecoveryCommitProof
			if e != nil || json.Unmarshal(data, &p) != nil {
				os.Exit(6)
			}
			a := finalAuthorizationForProof(p, in.Binding.AgentCredentialGeneration)
			wire, _ := json.Marshal(a)
			final, e := BeginRuntimeRecoveryFinalization(context.Background(), in.Dir, in.Binding, wire, in.Active, adapter)
			if e != nil {
				os.Exit(7)
			}
			defer final.Close()
			if mode := os.Getenv("RG_TEST_RECOVERY_FINAL_SUBMIT_CHILD"); mode != "" {
				if final.WithSubmission(context.Background(), in.Binding, markerDigest(wire), func(g *RuntimeRecoveryFinalizationGuard) error {
					p, e := g.PinProof(context.Background())
					if e != nil || mode == "pin" {
						return e
					}
					return g.RecordReceipt(context.Background(), receiptForFinalizationProof(p))
				}) != nil {
					os.Exit(8)
				}
			}
		}
		if os.Getenv("RG_TEST_RECOVERY_COMMIT_KILL") == "1" {
			if os.WriteFile(filepath.Join(in.Dir, "test-commit-ready"), []byte("ready"), 0600) != nil {
				os.Exit(4)
			}
			for {
				time.Sleep(20 * time.Millisecond)
			}
		}
		os.Exit(0) // No Close; the kernel drops flock, durable consumption survives.
	}
	e, a, b, g, wire := recoveryCommitFixture(t)
	input, _ := json.Marshal(struct {
		Dir, Active string
		Binding     RuntimeRecoveryGrantBinding
		Grant       RuntimeRecoveryMutationGrant
	}{e.receiptDir, e.activePath, b, g})
	cmd := exec.Command(os.Args[0], "-test.run=^TestRuntimeMutationRecoveryCommitProcessExit$")
	cmd.Env = append(os.Environ(), "RG_TEST_RECOVERY_COMMIT_CHILD="+string(input))
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Fatal("child consumption failed", err)
	}
	if runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryConsumptionFile)) {
		t.Fatal("crash lost consumption")
	}
	if runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoverySubmissionFile)) {
		t.Fatal("crash lost pinned submission")
	}
	if runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryAcknowledgementFile)) {
		t.Fatal("crash lost acknowledgement")
	}
	assertHistoricalRecoveryAfterExit(t, e.receiptDir, b)
	if m, err := BeginRuntimeRecoveryCommit(context.Background(), e.receiptDir, b, wire, e.activePath, a); err == nil {
		m.Close()
		t.Fatal("crash allowed nonce reuse")
	}
	if lease, err := BeginRuntimeMutation(e.receiptDir, g.Expected.TaskID, TaskKindVPNCoreService); err == nil {
		lease.Close()
		t.Fatal("crash unlocked mutation")
	}
}

func TestRuntimeMutationRecoveryCommitProcessKill(t *testing.T) {
	e, a, b, g, wire := recoveryCommitFixture(t)
	input, _ := json.Marshal(struct {
		Dir, Active string
		Binding     RuntimeRecoveryGrantBinding
		Grant       RuntimeRecoveryMutationGrant
	}{e.receiptDir, e.activePath, b, g})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRuntimeMutationRecoveryCommitProcessExit$")
	cmd.Env = append(os.Environ(), "RG_TEST_RECOVERY_COMMIT_CHILD="+string(input), "RG_TEST_RECOVERY_COMMIT_KILL=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	for runtimeMarkerAbsent(filepath.Join(e.receiptDir, "test-commit-ready")) {
		if ctx.Err() != nil {
			t.Fatal("child did not persist consumption and acquire lease")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("child unexpectedly exited successfully")
	}
	if runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryConsumptionFile)) {
		t.Fatal("SIGKILL lost consumption")
	}
	if runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoverySubmissionFile)) {
		t.Fatal("SIGKILL lost pinned submission")
	}
	if runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryAcknowledgementFile)) {
		t.Fatal("SIGKILL lost acknowledgement")
	}
	assertHistoricalRecoveryAfterExit(t, e.receiptDir, b)
	if m, err := BeginRuntimeRecoveryCommit(context.Background(), e.receiptDir, b, wire, e.activePath, a); err == nil {
		m.Close()
		t.Fatal("SIGKILL allowed nonce reuse")
	}
	if lease, err := BeginRuntimeMutation(e.receiptDir, g.Expected.TaskID, TaskKindVPNCoreService); err == nil {
		lease.Close()
		t.Fatal("SIGKILL unlocked mutation")
	}
}

func TestRuntimeMutationRecoveryConsumptionStopsLiveCompletion(t *testing.T) {
	e, task, _, _ := removalExecutorFixture(t)
	lease, err := BeginRuntimeMutation(e.receiptDir, task.ID, TaskKindVPNCoreService)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	// Fault injection: a record unexpectedly appears while an older owner is
	// live. It must not write a result or delete intent past recovery state.
	if err = os.WriteFile(filepath.Join(e.receiptDir, runtimeRecoveryConsumptionFile), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	b := RuntimeResultBinding{ManagerURL: "https://isolated.invalid", AgentID: task.AgentID, ServerID: task.ServerID}
	if lease.SaveResult(b, task.ID, []byte(`{"status":"succeeded"}`)) == nil {
		t.Fatal("result saved past consumption state")
	}
	if lease.Complete() == nil {
		t.Fatal("live completion removed recovery fence")
	}
	if runtimeMarkerAbsent(filepath.Join(e.receiptDir, "mutation-inflight.json")) {
		t.Fatal("intent disappeared")
	}
}

func TestRuntimeMutationRecoveryCommitZeroOwner(t *testing.T) {
	var owner RuntimeRecoveryCommit
	if _, err := owner.Proof(context.Background()); !errors.Is(err, ErrRuntimeMutationBlocked) {
		t.Fatal("zero owner issued proof", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}
