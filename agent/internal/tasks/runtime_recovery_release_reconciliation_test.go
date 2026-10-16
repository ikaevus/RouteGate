package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func preparedReleaseHistoryFixture(t *testing.T) (CredentialRemovalExecutor, RuntimeRecoveryGrantBinding, *RuntimeRecoveryFinalization) {
	t.Helper()
	e, _, b, _, m := releaseIntentFixture(t)
	if _, err := m.PrepareFenceRelease(context.Background(), b, m.authorizationHash); err != nil {
		t.Fatal(err)
	}
	m.Close()
	return e, b, m
}

func TestRuntimeMutationPreparedReleaseHistoricalReadOnly(t *testing.T) {
	e, a, b, auth := finalizationFixture(t)
	auth.ExpiresAt = time.Now().UTC().Add(4 * time.Second)
	wire, _ := json.Marshal(auth)
	m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	prepareRetirementFixture(t, m, b)
	if _, err = m.RetirePrepared(context.Background()); err != nil {
		t.Fatal(err)
	}
	confirmTerminalForReleasePlan(t, m, b)
	hash, err := m.PrepareFenceRelease(context.Background(), b, m.authorizationHash)
	if err != nil {
		t.Fatal(err)
	}
	if WithHistoricalRecoveryPreparedRelease(context.Background(), m.dir, b, func(RuntimeRecoveryPreparedReleaseHistory) error { t.Error("live owner bypass"); return nil }) != ErrRuntimeMutationBlocked {
		t.Fatal("reader ignored live owner")
	}
	frozen := m.terminalFrozen
	m.Close()
	time.Sleep(time.Until(auth.ExpiresAt) + 10*time.Millisecond)
	if err = os.Remove(e.activePath); err != nil {
		t.Fatal(err)
	}
	before := terminalHistoryTree(t, m.dir)
	lock, _ := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
	for range 2 {
		err = WithHistoricalRecoveryPreparedRelease(context.Background(), m.dir, b, func(h RuntimeRecoveryPreparedReleaseHistory) error {
			if h.ReleaseIntentHash != hash || h.TerminalProof != frozen || !h.AuthorizationExpiresAt.Equal(auth.ExpiresAt) {
				t.Error("expired history changed")
			}
			if WithHistoricalRecoveryPreparedRelease(context.Background(), m.dir, b, func(RuntimeRecoveryPreparedReleaseHistory) error { t.Error("nested reader acquired flock"); return nil }) != ErrRuntimeMutationBlocked {
				t.Error("nested reader bypass")
			}
			return nil
		})
		if err != nil {
			t.Fatal("expired history required runtime/renewal", err)
		}
	}
	assertTerminalHistoryTree(t, m.dir, before)
	after, _ := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
	if !os.SameFile(lock, after) {
		t.Fatal("reader recreated lock")
	}
	if _, err = BeginRuntimeRecoveryFinalization(context.Background(), m.dir, b, wire, e.activePath, a); err != ErrRuntimeMutationBlocked {
		t.Fatal("historical read resumed owner", err)
	}
	assertReleaseIntentFenced(t, m.dir, b)
}

func TestRuntimeMutationPreparedReleaseHistoricalRefusals(t *testing.T) {
	for _, mode := range []string{"missing_intent", "missing_ack", "schema", "purpose", "binding", "operation", "authorization", "expiry", "before_terminal", "at_expiry", "archive_name", "file_name", "file_hash", "file_order", "duplicate_key", "generation", "root_mode", "intent_mode", "intent_symlink", "intent_hardlink", "unknown_file", "partial_cleanup", "cancelled", "nil_callback", "callback_error", "changed_during_callback", "same_bytes_replaced", "ack_changed", "archive_changed", "cancel_during_callback"} {
		t.Run(mode, func(t *testing.T) {
			_, b, m := preparedReleaseHistoryFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			path := filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var r runtimeRecoveryReleaseIntent
			if !exactHistoricalJSON(original, &r) {
				t.Fatal("fixture")
			}
			write := func(path string, data []byte) {
				t.Helper()
				if e := os.WriteFile(path, data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			changed := true
			switch mode {
			case "schema":
				r.SchemaVersion = 2
			case "purpose":
				r.Purpose = "foreign"
			case "binding":
				r.Binding.ServerID = resultTaskID
			case "operation":
				r.OperationID = "ffffffff-ffff-4fff-8fff-ffffffffffff"
			case "authorization":
				r.AuthorizationHash = r.TerminalProofHash
			case "expiry":
				r.ExpiresAt = r.ExpiresAt.Add(time.Second)
			case "before_terminal":
				r.PreparedAt = m.terminalFrozen.ObservedAt.Add(-time.Nanosecond)
			case "at_expiry":
				r.PreparedAt = r.ExpiresAt
			case "archive_name":
				r.ArchiveName = "../foreign"
			case "file_name":
				r.Files[0].Name = "../foreign"
			case "file_hash":
				r.Files[0].Hash = r.TerminalProofHash
			case "file_order":
				r.Files[0], r.Files[1] = r.Files[1], r.Files[0]
			default:
				changed = false
			}
			if changed {
				data, _ := json.Marshal(r)
				write(path, data)
			}
			switch mode {
			case "missing_intent":
				os.Remove(path)
			case "missing_ack":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile))
			case "duplicate_key":
				write(path, append([]byte(`{"schemaVersion":1,`), original[1:]...))
			case "generation":
				b.AgentCredentialGeneration++
			case "root_mode":
				os.Chmod(m.dir, 0755)
			case "intent_mode":
				os.Chmod(path, 0644)
			case "intent_symlink":
				os.Remove(path)
				if err := os.Symlink(filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile), path); err != nil {
					t.Fatal(err)
				}
			case "intent_hardlink":
				if err := os.Link(path, filepath.Join(t.TempDir(), "alias")); err != nil {
					t.Fatal(err)
				}
			case "unknown_file":
				write(filepath.Join(m.dir, "unknown.json"), []byte("{}"))
			case "partial_cleanup":
				data, err := os.ReadFile(filepath.Join(m.archive, runtimeResultFile))
				if err != nil {
					t.Fatal(err)
				}
				write(filepath.Join(m.dir, runtimeResultFile), data)
			case "cancelled":
				cancel()
			}
			before := terminalHistoryTree(t, m.dir)
			called := false
			fn := func(RuntimeRecoveryPreparedReleaseHistory) error {
				called = true
				switch mode {
				case "callback_error":
					return errors.New("secret fixture error")
				case "changed_during_callback":
					write(path, []byte("{}"))
					before[runtimeRecoveryReleaseIntentFile] = []byte("{}")
				case "same_bytes_replaced":
					tmp := filepath.Join(m.dir, ".replacement")
					write(tmp, original)
					if err := os.Rename(tmp, path); err != nil {
						t.Fatal(err)
					}
				case "ack_changed":
					write(filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile), []byte("{}"))
					before[runtimeRecoveryTerminalAcknowledgementFile] = []byte("{}")
				case "archive_changed":
					write(filepath.Join(m.archive, runtimeRecoveryFinalSubmissionFile), []byte("{}"))
					before[filepath.Join(filepath.Base(m.archive), runtimeRecoveryFinalSubmissionFile)] = []byte("{}")
				case "cancel_during_callback":
					cancel()
				default:
					t.Error("invalid history reached callback")
				}
				return nil
			}
			if mode == "nil_callback" {
				fn = nil
			}
			if err := WithHistoricalRecoveryPreparedRelease(ctx, m.dir, b, fn); err != ErrRuntimeMutationBlocked {
				t.Fatal("unsafe history accepted", err)
			}
			if (mode == "callback_error" || mode == "changed_during_callback" || mode == "same_bytes_replaced" || mode == "ack_changed" || mode == "archive_changed" || mode == "cancel_during_callback") && !called {
				t.Fatal("test missed callback cut")
			}
			assertTerminalHistoryTree(t, m.dir, before)
		})
	}
}
