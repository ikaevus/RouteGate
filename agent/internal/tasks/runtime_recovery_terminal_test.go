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

func terminalFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, RuntimeRecoveryReleaseAuthorization, *RuntimeRecoveryFinalization) {
	t.Helper()
	e, a, b, auth, m := retirementFixture(t)
	prepareRetirementFixture(t, m, b)
	return e, a, b, auth, m
}
func assertTerminalArchive(t *testing.T, dir string, b RuntimeRecoveryGrantBinding) {
	t.Helper()
	data, e := os.ReadFile(filepath.Join(dir, runtimeRecoveryTerminalFile))
	if e != nil {
		t.Fatal(e)
	}
	var r runtimeRecoveryTerminalRecord
	if !exactHistoricalJSON(data, &r) {
		t.Fatal("terminal wire")
	}
	archive, e := retirementArchivePath(dir, r.OperationID)
	if e != nil || privateRetirementArchive(archive) != nil {
		t.Fatal("archive", e)
	}
	legacy, e := os.ReadFile(filepath.Join(dir, "mutation-inflight.json"))
	archived, e2 := os.ReadFile(filepath.Join(archive, "mutation-inflight.json"))
	if e != nil || e2 != nil || !bytes.Equal(legacy, archived) {
		t.Fatal("legacy compatibility fence lost", e, e2)
	}
	if WithHistoricalRecoveryFinalization(context.Background(), dir, b, func(p RuntimeRecoveryFinalizationProof) error {
		wire, _ := json.Marshal(p)
		if markerDigest(wire) != r.FinalProofHash {
			t.Error("historical proof drift")
		}
		return nil
	}) != nil {
		t.Fatal("terminal historical read")
	}
	if lease, e := BeginRuntimeMutation(dir, r.Observation.TaskID, TaskKindVPNCoreService); e == nil {
		lease.Close()
		t.Fatal("terminal archive released fence")
	}
}
func TestRuntimeMutationTerminalRetirementRetained(t *testing.T) {
	e, a, b, auth, m := terminalFixture(t)
	manifest, err := retirementSourceManifest(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, file := range manifest {
		if file.Present {
			before[file.Name], err = os.ReadFile(filepath.Join(m.dir, file.Name))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	lockInfo, _ := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
	intent, _ := os.ReadFile(filepath.Join(m.dir, runtimeRecoveryRetirementFile))
	hash, err := m.RetirePrepared(context.Background())
	if err != nil || hash == "" {
		t.Fatal("terminal retirement", err)
	}
	terminal, _ := os.ReadFile(filepath.Join(m.dir, runtimeRecoveryTerminalFile))
	if hash != markerDigest(terminal) {
		t.Fatal("terminal digest")
	}
	for name, want := range before {
		if name == "mutation-inflight.json" {
			got, e := os.ReadFile(filepath.Join(m.dir, name))
			if e != nil || !bytes.Equal(got, want) {
				t.Fatal("legacy fence changed", e)
			}
		}
		if name != "mutation-inflight.json" && !runtimeMarkerAbsent(filepath.Join(m.dir, name)) {
			t.Fatal("active copy retained", name)
		}
		got, err := os.ReadFile(filepath.Join(m.archive, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("archive lost history", name, err)
		}
	}
	got, _ := os.ReadFile(filepath.Join(m.dir, runtimeRecoveryRetirementFile))
	if !bytes.Equal(got, intent) {
		t.Fatal("journal changed")
	}
	newLock, _ := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
	if !os.SameFile(lockInfo, newLock) {
		t.Fatal("flock namespace moved")
	}
	repeat, err := m.RetirePrepared(context.Background())
	if err != nil || repeat != hash {
		t.Fatal("terminal repeat", err)
	}
	got, _ = os.ReadFile(filepath.Join(m.dir, runtimeRecoveryTerminalFile))
	if !bytes.Equal(got, terminal) {
		t.Fatal("terminal renewed time/bytes")
	}
	if _, err = m.Proof(context.Background()); err != ErrRuntimeMutationBlocked {
		t.Fatal("retired owner submitted live final proof", err)
	}
	if a.restarts != 1 || a.validations != 0 {
		t.Fatal("terminal metadata mutated runtime")
	}
	if lock, e := lockRuntimeMutation(m.dir); e == nil {
		lock.Close()
		t.Fatal("retirement dropped live flock")
	}
	m.Close()
	assertTerminalArchive(t, m.dir, b)
	if _, err = BeginRuntimeRecoveryFinalization(context.Background(), m.dir, b, mustRetirementJSON(t, auth), e.activePath, a); err != ErrRuntimeMutationBlocked {
		t.Fatal("terminal owner resumed", err)
	}
}

func TestRuntimeMutationTerminalRetirementRejects(t *testing.T) {
	for _, mode := range []string{"unprepared", "closed", "cancelled", "expired", "backwards", "runtime", "existing_archive", "archive_symlink", "journal_drift", "publication_expiry", "publication_runtime", "terminal_drift", "cleanup_expiry", "cleanup_runtime", "cleanup_error"} {
		t.Run(mode, func(t *testing.T) {
			e, a, b, auth, m := retirementFixture(t)
			if mode != "unprepared" {
				prepareRetirementFixture(t, m, b)
			}
			ctx := context.Background()
			clock := time.Now().UTC()
			m.now = func() time.Time { return clock }
			archive, _ := retirementArchivePath(m.dir, auth.OperationID)
			switch mode {
			case "closed":
				m.Close()
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "expired":
				clock = auth.ExpiresAt
			case "backwards":
				clock = clock.Add(-time.Second)
			case "runtime":
				os.WriteFile(e.activePath, []byte(`{}`), 0600)
			case "existing_archive":
				os.Mkdir(archive, 0700)
			case "archive_symlink":
				os.Symlink(t.TempDir(), archive)
			case "journal_drift":
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryRetirementFile), []byte(`{}`), 0600)
			}
			checkpoint := func(step string) error {
				if step == "archive" {
					switch mode {
					case "publication_expiry":
						clock = auth.ExpiresAt
					case "publication_runtime":
						a.generation.PID++
					}
				}
				if step == "terminal" && mode == "terminal_drift" {
					return os.WriteFile(filepath.Join(m.dir, runtimeRecoveryTerminalFile), []byte(`{}`), 0600)
				}
				if strings.HasPrefix(step, "cleanup-") {
					switch mode {
					case "cleanup_expiry":
						clock = auth.ExpiresAt
					case "cleanup_runtime":
						a.generation.PID++
					case "cleanup_error":
						return errors.New("secret-payload")
					}
				}
				return nil
			}
			hash, err := m.retirePrepared(ctx, checkpoint)
			if err != ErrRuntimeMutationBlocked || hash != "" {
				t.Fatal("unsafe terminal accepted", err)
			}
			if lease, e := BeginRuntimeMutation(m.dir, auth.Expected.TaskID, TaskKindVPNCoreService); e == nil {
				lease.Close()
				t.Fatal("refusal released fence")
			}
			if strings.HasPrefix(mode, "cleanup_") {
				m.Close()
				assertTerminalArchive(t, m.dir, b)
			}
		})
	}
}

func TestRuntimeMutationTerminalHistoryRejects(t *testing.T) {
	for _, mode := range []string{"schema", "purpose", "operation", "binding", "intent", "proof", "receipt", "time", "observation", "duplicate", "alias", "unknown", "archive_permissions", "archive_extra", "archive_symlink", "archive_tamper", "archive_hardlink", "root_leftover", "root_journal", "changed_during_read"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, auth, m := terminalFixture(t)
			if _, err := m.RetirePrepared(context.Background()); err != nil {
				t.Fatal(err)
			}
			m.Close()
			path := filepath.Join(m.dir, runtimeRecoveryTerminalFile)
			data, _ := os.ReadFile(path)
			var r runtimeRecoveryTerminalRecord
			json.Unmarshal(data, &r)
			switch mode {
			case "schema":
				r.SchemaVersion = 2
			case "purpose":
				r.Purpose = "released"
			case "operation":
				r.OperationID = "../escape"
			case "binding":
				r.Binding.AgentCredentialGeneration++
			case "intent":
				r.IntentHash = strings.Repeat("f", 64)
			case "proof":
				r.FinalProofHash = strings.Repeat("f", 64)
			case "receipt":
				r.FinalReceiptHash = strings.Repeat("f", 64)
			case "time":
				r.ObservedAt = auth.ExpiresAt
			case "observation":
				r.Observation.Witness.ListenerPort++
			case "archive_permissions":
				os.Chmod(m.archive, 0755)
			case "archive_extra":
				os.WriteFile(filepath.Join(m.archive, "untrusted"), []byte(`{}`), 0600)
			case "archive_symlink":
				os.Rename(m.archive, m.archive+"-saved")
				os.Symlink(m.archive+"-saved", m.archive)
			case "archive_tamper":
				os.WriteFile(filepath.Join(m.archive, runtimeRecoveryFinalSubmissionFile), []byte(`{}`), 0600)
			case "archive_hardlink":
				os.Link(filepath.Join(m.archive, runtimeRecoveryFinalSubmissionFile), filepath.Join(m.dir, "extra-link"))
			case "root_leftover":
				os.WriteFile(filepath.Join(m.dir, "mutation-inflight.json"), []byte(`{}`), 0600)
			case "root_journal":
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryRetirementFile), []byte(`{}`), 0600)
			}
			data = mustRetirementJSON(t, r)
			switch mode {
			case "duplicate":
				data = append([]byte(`{"schemaVersion":2,`), data[1:]...)
			case "alias":
				data = bytes.Replace(data, []byte(`"schemaVersion"`), []byte(`"SchemaVersion"`), 1)
			case "unknown":
				data = append([]byte(`{"secret":"payload",`), data[1:]...)
			}
			os.WriteFile(path, data, 0600)
			called := false
			err := WithHistoricalRecoveryFinalization(context.Background(), m.dir, b, func(RuntimeRecoveryFinalizationProof) error {
				called = true
				if mode == "changed_during_read" {
					os.WriteFile(path, []byte(`{}`), 0600)
				}
				return nil
			})
			if err != ErrRuntimeMutationBlocked || called != (mode == "changed_during_read") {
				t.Fatal("unsafe terminal history", err, called)
			}
		})
	}
}

func TestRuntimeMutationTerminalCleanupInterruptedPreservesArchive(t *testing.T) {
	_, _, b, _, m := terminalFixture(t)
	_, err := m.retirePrepared(context.Background(), func(step string) error {
		if step == "cleanup-"+runtimeResultFile {
			return errors.New("uncertain")
		}
		return nil
	})
	if err != ErrRuntimeMutationBlocked {
		t.Fatal(err)
	}
	if _, err = m.RetirePrepared(context.Background()); err != ErrRuntimeMutationBlocked {
		t.Fatal("poisoned cleanup resumed", err)
	}
	m.Close()
	assertTerminalArchive(t, m.dir, b)
}

func TestRuntimeMutationTerminalRetirementConcurrent(t *testing.T) {
	_, _, b, _, m := terminalFixture(t)
	type result struct {
		hash string
		err  error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() { h, e := m.RetirePrepared(context.Background()); results <- result{h, e} }()
	}
	a, c := <-results, <-results
	if a.err != nil || c.err != nil || a.hash == "" || a.hash != c.hash {
		t.Fatal("concurrent retirement diverged", a.err, c.err)
	}
	m.Close()
	assertTerminalArchive(t, m.dir, b)
}

func TestRuntimeMutationTerminalRetirementCloseWaits(t *testing.T) {
	_, _, b, _, m := terminalFixture(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	unblock := func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	defer unblock()
	go func() {
		_, e := m.retirePrepared(context.Background(), func(step string) error {
			if step == "copy-mutation-inflight.json" {
				close(entered)
				<-release
			}
			return nil
		})
		done <- e
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("retirement did not enter copy")
	}
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	select {
	case <-closed:
		t.Fatal("Close interrupted retirement")
	case <-time.After(25 * time.Millisecond):
	}
	if lock, e := lockRuntimeMutation(m.dir); e == nil {
		lock.Close()
		t.Fatal("Close dropped flock")
	}
	unblock()
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if e := <-closed; e != nil {
		t.Fatal(e)
	}
	assertTerminalArchive(t, m.dir, b)
}
