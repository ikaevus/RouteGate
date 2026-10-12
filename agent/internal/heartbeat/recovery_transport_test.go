package heartbeat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/config"
	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func TestRuntimeMutationRecoveryTransportRetainsFence(t *testing.T) {
	for _, mode := range []string{"accepted", "lost_ack", "wrong_ack", "unready", "wrong_identity", "expired", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			var challenge tasks.RuntimeRecoveryChallenge
			var bodies [][]byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer rg_agent_isolated" {
					t.Error("missing auth")
				}
				switch {
				case r.URL.Path == "/api/v1/agent/tasks/next":
					payload := map[string]any{"recoveryChallenge": challenge}
					if mode == "ambiguous" {
						payload["task"] = tasks.ConfigTask{ID: fencedTaskID, Kind: tasks.TaskKindVPNCoreService}
					}
					_ = json.NewEncoder(w).Encode(payload)
				case strings.HasSuffix(r.URL.Path, "/evidence"):
					data, _ := io.ReadAll(r.Body)
					bodies = append(bodies, data)
					if mode == "lost_ack" && len(bodies) == 1 {
						http.Error(w, "sensitive-server-payload", 500)
						return
					}
					var response tasks.RuntimeRecoveryChallengeResponse
					if json.Unmarshal(data, &response) != nil || response.Observation != challenge.Expected {
						t.Error("wrong evidence")
					}
					sum := sha256.Sum256(data)
					hash := hex.EncodeToString(sum[:])
					if mode == "wrong_ack" {
						hash = strings.Repeat("0", 64)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"schemaVersion": 1, "accepted": true, "operationId": challenge.OperationID, "challengeHash": response.ChallengeHash, "responseHash": hash})
				default:
					t.Error("unexpected task/mutation delivery")
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			r := NewRunner(config.Config{ManagerURL: srv.URL, AgentID: fencedAgentID, ServerID: fencedServerID, AgentToken: "rg_agent_isolated", ServiceControlEnabled: true, ExperimentalRuntimeMutationFencing: true}, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
			r.runtimeMutationDir = filepath.Join(t.TempDir(), "mutations")
			r.cfg.ActiveConfigPath = filepath.Join(t.TempDir(), "runtime.json")
			if err := os.WriteFile(r.cfg.ActiveConfigPath, []byte(`{"inbounds":[]}`), 0600); err != nil {
				t.Fatal(err)
			}
			adapter := &recoveryServiceAdapter{fencedServiceAdapter: &fencedServiceAdapter{}, restarted: true}
			r.vpnCoreAdapter = adapter
			r.runtimeMutationManagerReady = mode != "unready"
			lease, err := tasks.BeginRuntimeMutation(r.runtimeMutationDir, fencedTaskID, tasks.TaskKindVPNCoreService)
			if err != nil {
				t.Fatal(err)
			}
			witness, err := tasks.CaptureRuntimeRecoveryWitness(context.Background(), r.cfg.ActiveConfigPath, adapter)
			if err != nil {
				t.Fatal(err)
			}
			witness.PreviousGeneration = tasks.RuntimeGeneration{InvocationID: strings.Repeat("a", 32), PID: 123, StartedMonotonic: "500"}
			if err = lease.SaveResultWithRuntimeWitness(r.runtimeResultBinding(), fencedTaskID, []byte(`{"status":"succeeded","resultPayload":{}}`), &witness); err != nil {
				t.Fatal(err)
			}
			lease.Close()
			if err = tasks.ReplayRuntimeResult(r.runtimeMutationDir, r.runtimeResultBinding(), func(string, []byte) error { return nil }); err != nil {
				t.Fatal(err)
			}
			o, err := tasks.ObserveRuntimeRecovery(context.Background(), r.runtimeMutationDir, r.runtimeResultBinding(), fencedTaskID, r.cfg.ActiveConfigPath, adapter)
			if err != nil {
				t.Fatal(err)
			}
			challenge = tasks.RuntimeRecoveryChallenge{SchemaVersion: 1, OperationID: "44444444-4444-4444-8444-444444444444", Nonce: strings.Repeat("d", 64), IssuedAt: time.Now().UTC().Add(-time.Second), ExpiresAt: time.Now().UTC().Add(time.Minute), Expected: o}
			if mode == "wrong_identity" {
				challenge.Expected.AgentID = challenge.OperationID
			}
			if mode == "expired" {
				challenge.ExpiresAt = time.Now().Add(-time.Second)
			}
			paths := []string{r.cfg.ActiveConfigPath, filepath.Join(r.runtimeMutationDir, "mutation-inflight.json"), filepath.Join(r.runtimeMutationDir, "mutation-result.json")}
			before := make([][]byte, len(paths))
			for i, p := range paths {
				before[i], err = os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
			}
			err = r.processNextTask(context.Background())
			ok := mode == "accepted" || mode == "lost_ack"
			if (err == nil) != ok {
				t.Fatal("incorrect outcome", err)
			}
			if err != nil && strings.Contains(err.Error(), "sensitive") {
				t.Fatal("payload leaked")
			}
			want := 0
			if ok || mode == "wrong_ack" {
				want = 1
			}
			if mode == "lost_ack" {
				want = 2
			}
			if len(bodies) != want {
				t.Fatal("unexpected attempts", len(bodies), want)
			}
			if len(bodies) == 2 && !bytes.Equal(bodies[0], bodies[1]) {
				t.Fatal("retry changed evidence")
			}
			for i, p := range paths {
				after, e := os.ReadFile(p)
				if e != nil || !bytes.Equal(after, before[i]) {
					t.Fatal("transport changed runtime/receipt/fence", e)
				}
			}
			if adapter.calls != 0 {
				t.Fatal("runtime operation executed")
			}
			if l, e := tasks.BeginRuntimeMutation(r.runtimeMutationDir, fencedTaskID, tasks.TaskKindVPNCoreService); e == nil {
				l.Close()
				t.Fatal("evidence unlocked fence")
			}
		})
	}
}
