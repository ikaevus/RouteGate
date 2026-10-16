package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func releaseIntentFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, RuntimeRecoveryReleaseAuthorization, *RuntimeRecoveryFinalization) {
	t.Helper()
	e, a, b, auth, m := terminalSubmissionFixture(t)
	confirmTerminalForReleasePlan(t, m, b)
	return e, a, b, auth, m
}

func assertReleaseIntentFenced(t *testing.T, dir string, b RuntimeRecoveryGrantBinding) {
	t.Helper()
	before := terminalHistoryTree(t, dir)
	var r runtimeRecoveryReleaseIntent
	data, err := readTerminalConfirmationFile(filepath.Join(dir, runtimeRecoveryReleaseIntentFile), 8192)
	if err != nil || !exactHistoricalJSON(data, &r) || r.SchemaVersion != 1 || r.Purpose != runtimeRecoveryReleaseIntentPurpose || r.Binding != b || !r.PreparedAt.Before(r.ExpiresAt) {
		t.Fatal("lost private release intent", err)
	}
	archive := filepath.Join(dir, r.ArchiveName)
	if privateRetirementArchive(archive) != nil {
		t.Fatal("archive changed")
	}
	for _, f := range r.Files {
		if markerDigest(before[f.Name]) != f.Hash {
			t.Fatal("retained root file changed", f.Name)
		}
	}
	legacy, err := os.ReadFile(filepath.Join(archive, "mutation-inflight.json"))
	if err != nil || !bytes.Equal(legacy, before["mutation-inflight.json"]) {
		t.Fatal("legacy fence changed", err)
	}
	if l, e := BeginRuntimeMutation(dir, resultTaskID, TaskKindVPNCoreService); e == nil {
		l.Close()
		t.Fatal("release intent admitted mutation")
	}
	if e := ReplayRuntimeResult(dir, b.RuntimeResultBinding, func(string, []byte) error { t.Error("unexpected replay"); return nil }); e != ErrRuntimeMutationBlocked {
		t.Fatal("release intent admitted result replay", e)
	}
	if e := WithHistoricalRecoveryTerminal(context.Background(), dir, b, func(RuntimeRecoveryTerminalProof) error {
		t.Error("intent adopted by historical terminal reader")
		return nil
	}); e != ErrRuntimeMutationBlocked {
		t.Fatal(e)
	}
	if e := WithHistoricalRecoveryFinalization(context.Background(), dir, b, func(RuntimeRecoveryFinalizationProof) error {
		t.Error("intent ignored by historical final reader")
		return nil
	}); e != ErrRuntimeMutationBlocked {
		t.Fatal(e)
	}
	assertTerminalHistoryTree(t, dir, before)
}

func TestRuntimeMutationFenceReleaseIntentDurable(t *testing.T) {
	_, _, b, _, m := releaseIntentFixture(t)
	defer m.Close()
	before := terminalHistoryTree(t, m.dir)
	p, err := m.PlanFenceRelease(context.Background(), b, m.authorizationHash)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := m.PrepareFenceRelease(context.Background(), b, m.authorizationHash)
	if err != nil {
		t.Fatal(err)
	}
	after := terminalHistoryTree(t, m.dir)
	data := after[runtimeRecoveryReleaseIntentFile]
	var r runtimeRecoveryReleaseIntent
	if !exactHistoricalJSON(data, &r) || r != releaseIntentForPlan(p, r.PreparedAt) || digest != markerDigest(data) || !bytes.Equal(m.releaseIntent, data) {
		t.Fatal("intent bindings")
	}
	delete(after, runtimeRecoveryReleaseIntentFile)
	if len(after) != len(before) {
		t.Fatal("publication changed root set")
	}
	for path, data := range before {
		if !bytes.Equal(after[path], data) {
			t.Fatal("publication changed history", path)
		}
	}
	// Concurrent same-owner retries recheck live readiness, without rewriting or
	// changing PreparedAt, file identity, original expiry or digest.
	info, _ := os.Lstat(filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile))
	results := make(chan string, 3)
	for range 3 {
		go func() {
			h, e := m.PrepareFenceRelease(context.Background(), b, m.authorizationHash)
			if e != nil {
				results <- "error"
			} else {
				results <- h
			}
		}()
	}
	for range 3 {
		if h := <-results; h != digest {
			t.Fatal("retry changed intent")
		}
	}
	current, _ := os.Lstat(filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile))
	if !os.SameFile(info, current) {
		t.Fatal("retry replaced intent inode")
	}
	if got, e := m.PlanFenceRelease(context.Background(), b, m.authorizationHash); e != nil || got != p {
		t.Fatal("prepared owner lost read-only inventory", e)
	}
	if _, e := m.RetirePrepared(context.Background()); e != ErrRuntimeMutationBlocked {
		t.Fatal("prepared phase reentered archival cleanup")
	}
	if e := m.WithTerminalSubmission(context.Background(), b, m.authorizationHash, func(*RuntimeRecoveryTerminalGuard) error { t.Error("prepared phase reentered submission"); return nil }); e != ErrRuntimeMutationBlocked {
		t.Fatal(e)
	}
	m.Close()
	assertReleaseIntentFenced(t, m.dir, b)
	if h, e := m.PrepareFenceRelease(context.Background(), b, m.authorizationHash); e != ErrRuntimeMutationBlocked || h != "" {
		t.Fatal("closed owner resumed intent")
	}
}

func TestRuntimeMutationFenceReleaseIntentRefusals(t *testing.T) {
	for _, mode := range []string{"generation", "authorization", "missing_ack", "expired", "cancelled", "runtime", "foreign_intent", "identical_intent", "publication_foreign_race", "publication_identical_race", "post_write_failure", "post_write_cancel", "post_write_expiry", "post_write_runtime", "prepared_tamper", "prepared_removed", "prepared_hardlink", "prepared_mode"} {
		t.Run(mode, func(t *testing.T) {
			e, _, b, auth, m := releaseIntentFixture(t)
			defer m.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			path := filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile)
			hash := m.authorizationHash
			write := func(data []byte) {
				t.Helper()
				if e := os.WriteFile(path, data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			p, err := m.PlanFenceRelease(ctx, b, hash)
			if err != nil {
				t.Fatal(err)
			}
			if len(mode) > 9 && mode[:9] == "prepared_" {
				if _, e := m.PrepareFenceRelease(ctx, b, hash); e != nil {
					t.Fatal(e)
				}
			}
			var checkpoint func() error
			switch mode {
			case "generation":
				b.AgentCredentialGeneration++
			case "authorization":
				hash = "foreign"
			case "missing_ack":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile))
			case "expired":
				m.now = func() time.Time { return auth.ExpiresAt }
			case "cancelled":
				cancel()
			case "runtime":
				if e := os.WriteFile(e.activePath, []byte("{}"), 0600); e != nil {
					t.Fatal(e)
				}
			case "foreign_intent":
				write([]byte("{}"))
			case "identical_intent":
				data, _ := json.Marshal(releaseIntentForPlan(p, m.lastChecked))
				write(data)
			case "publication_foreign_race", "publication_identical_race":
				calls := 0
				clock := m.lastChecked.Add(time.Millisecond)
				m.now = func() time.Time {
					calls++
					if calls == 6 {
						data := []byte("{}")
						if mode == "publication_identical_race" {
							data, _ = json.Marshal(releaseIntentForPlan(p, clock))
						}
						write(data)
					}
					return clock
				}
			case "post_write_failure":
				checkpoint = func() error { return errors.New("injected") }
			case "post_write_cancel":
				checkpoint = func() error { cancel(); return nil }
			case "post_write_expiry":
				checkpoint = func() error { m.now = func() time.Time { return auth.ExpiresAt }; return nil }
			case "post_write_runtime":
				checkpoint = func() error { return os.WriteFile(e.activePath, []byte("{}"), 0600) }
			case "prepared_tamper":
				write([]byte("{}"))
			case "prepared_removed":
				os.Remove(path)
			case "prepared_hardlink":
				if e := os.Link(path, filepath.Join(t.TempDir(), "copy")); e != nil {
					t.Fatal(e)
				}
			case "prepared_mode":
				os.Chmod(path, 0644)
			}
			before := terminalHistoryTree(t, m.dir)
			h, err := m.prepareFenceRelease(ctx, b, hash, checkpoint)
			if err != ErrRuntimeMutationBlocked || h != "" {
				t.Fatal("unsafe intent accepted", err)
			}
			if mode == "publication_foreign_race" || mode == "publication_identical_race" {
				if m.releaseIntent != nil || !m.poisoned {
					t.Fatal("publication race adopted disk state")
				}
				data, e := os.ReadFile(path)
				if e != nil {
					t.Fatal("publication race erased winner", e)
				}
				if mode == "publication_foreign_race" && string(data) != "{}" {
					t.Fatal("overwrote foreign intent")
				}
				if mode == "publication_identical_race" {
					want, _ := json.Marshal(releaseIntentForPlan(p, m.lastChecked))
					if !bytes.Equal(data, want) {
						t.Fatal("overwrote identical intent")
					}
				}
			} else if len(mode) > 11 && mode[:11] == "post_write_" {
				if runtimeMarkerAbsent(path) || !m.poisoned {
					t.Fatal("uncertainty erased intent or kept owner usable")
				}
			} else {
				assertTerminalHistoryTree(t, m.dir, before)
			}
		})
	}
}

func TestRuntimeMutationFenceReleaseIntentAloneGuards(t *testing.T) {
	for _, data := range []string{"", "{}", "malformed"} {
		t.Run(data, func(t *testing.T) {
			e, task, a, _ := removalExecutorFixture(t)
			lease, err := BeginRuntimeMutation(e.receiptDir, task.ID, TaskKindVPNCoreService)
			if err != nil {
				t.Fatal(err)
			}
			if e := os.WriteFile(filepath.Join(e.receiptDir, runtimeRecoveryReleaseIntentFile), []byte(data), 0600); e != nil {
				t.Fatal(e)
			}
			if lease.Complete() != ErrRuntimeMutationBlocked {
				t.Fatal("intent alone allowed completion")
			}
			if lease.SaveResult(RuntimeResultBinding{}, task.ID, []byte(`{"status":"succeeded"}`)) != ErrRuntimeMutationBlocked {
				t.Fatal("intent alone allowed result save")
			}
			lease.Close()
			if e := os.Remove(filepath.Join(e.receiptDir, "mutation-inflight.json")); e != nil {
				t.Fatal(e)
			}
			if l, e := BeginRuntimeMutation(e.receiptDir, task.ID, TaskKindVPNCoreService); e == nil {
				l.Close()
				t.Fatal("intent alone allowed admission")
			}
			if _, e := e.Execute(context.Background(), task); e == nil || a.restarts != 0 {
				t.Fatal("intent alone allowed removal")
			}
			if e := ReplayRuntimeResult(e.receiptDir, RuntimeResultBinding{}, func(string, []byte) error { t.Error("unexpected delivery"); return nil }); e == nil {
				t.Fatal("intent alone allowed replay")
			}
		})
	}
}

func TestRuntimeMutationFenceReleaseIntentInvalidatesHistoricalCallback(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "final", true: "terminal"}[terminal], func(t *testing.T) {
			_, _, b, _, m := terminalFixture(t)
			if terminal {
				if _, err := m.RetirePrepared(context.Background()); err != nil {
					t.Fatal(err)
				}
				confirmTerminalForReleasePlan(t, m, b)
			}
			m.Close()
			before := terminalHistoryTree(t, m.dir)
			inject := func() error {
				return os.WriteFile(filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile), []byte("{}"), 0600)
			}
			var err error
			if terminal {
				err = WithHistoricalRecoveryTerminal(context.Background(), m.dir, b, func(RuntimeRecoveryTerminalProof) error { return inject() })
			} else {
				err = WithHistoricalRecoveryFinalization(context.Background(), m.dir, b, func(RuntimeRecoveryFinalizationProof) error { return inject() })
			}
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("new phase ignored during historical callback", err)
			}
			before[runtimeRecoveryReleaseIntentFile] = []byte("{}")
			assertTerminalHistoryTree(t, m.dir, before)
		})
	}
}
