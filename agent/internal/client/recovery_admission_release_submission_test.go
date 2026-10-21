package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

// Exercise actual local unlink/fsync followed by the authenticated Manager
// exchange in the SAME original-owner callback. The fake Manager records
// claims only; actual PostgreSQL/traffic coordination is deliberately absent.
func TestRecoveryAdmissionReleaseOriginalOwnerSubmission(t *testing.T) {
	for _, mode := range []string{"recorded", "manager_rejected", "forged_receipt"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			var dir string
			var v VerifiedRecoveryReleaseAuthorization
			var prepared tasks.RuntimeRecoveryPreparedReleaseProof
			var retained tasks.RuntimeRecoveryRetentionProof
			var cleaned tasks.RuntimeRecoveryCleanedCommitProof
			var managerPosts atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("unauthenticated submission")
				}
				f, err := os.OpenFile(filepath.Join(dir, "runtime.lock"), os.O_RDWR, 0)
				if err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				defer f.Close()
				err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
				if err == nil {
					syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				}
				if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
					t.Error("HTTP escaped original flock", err)
				}
				w.Header().Set("Cache-Control", "no-store")
				if r.URL.Path == "/api/v1/agent/identity/verify" {
					var q credentialIdentityRequest
					json.Unmarshal(data, &q)
					json.NewEncoder(w).Encode(credentialIdentityReply{1, true, q.AgentID, q.ServerID, v.Binding.AgentCredentialGeneration, recoveryReadHash(data)})
					return
				}
				base := "/api/v1/agent/recovery-observations/" + prepared.OperationID
				switch r.URL.Path {
				case base + "/prepared-release/verify":
					h := recoveryReadHash(data)
					json.NewEncoder(w).Encode(RecoveryPreparedReleaseReceipt{SchemaVersion: 1, Recorded: true, OperationID: prepared.OperationID, ReleaseIntentHash: prepared.ReleaseIntentHash, ProofHash: h, RequestHash: h})
				case base + "/release-authorization":
					json.NewEncoder(w).Encode(recoveryReleaseReply{1, prepared.OperationID, v.Binding.AgentID, v.Binding.ServerID, v.Binding.AgentCredentialGeneration, recoveryReadHash(data), v.AuthorizationHash, v.Authorization})
				case base + "/retention/verify":
					h := recoveryReadHash(data)
					json.NewEncoder(w).Encode(RecoveryRetentionReceipt{SchemaVersion: 1, Recorded: true, OperationID: retained.OperationID, RetentionHash: retained.RetentionHash, ProofHash: h, RequestHash: h})
				case base + "/cleaned-commit/verify":
					wire, _ := json.Marshal(cleaned)
					if !bytes.Equal(wire, data) {
						t.Error("cleaned predecessor changed")
					}
					h := recoveryReadHash(data)
					json.NewEncoder(w).Encode(RecoveryCleanedCommitReceipt{SchemaVersion: 1, Recorded: true, OperationID: cleaned.OperationID, CleanedIntentHash: cleaned.CleanedIntentHash, ProofHash: h, RequestHash: h})
				case base + "/admission-release", base + "/admission-release/verify":
					if strings.HasSuffix(r.URL.Path, "/admission-release") {
						managerPosts.Add(1)
					}
					var p tasks.RuntimeRecoveryAdmissionReleaseProof
					if json.Unmarshal(data, &p) != nil || p.SchemaVersion != 1 || p.OperationID != prepared.OperationID || p.AgentCredentialGeneration != v.Binding.AgentCredentialGeneration || p.AuthorizationHash != v.AuthorizationHash || p.Observation != cleaned.Observation || !p.ReleasedAt.After(cleaned.ObservedAt) {
						t.Error("unbound post-release claim")
					}
					local, e := os.ReadFile(dir + "-admission-proof-" + p.OperationID + ".json")
					if e != nil || !bytes.Equal(local, data) {
						t.Error("proof sent without pinned local publication", e)
					}
					if mode == "manager_rejected" {
						w.WriteHeader(http.StatusConflict)
						return
					}
					h := recoveryReadHash(data)
					if mode == "forged_receipt" {
						h = strings.Repeat("f", 64)
					}
					json.NewEncoder(w).Encode(RecoveryAdmissionReleaseReceipt{1, true, prepared.OperationID, h, h})
				default:
					t.Error("unexpected Manager route", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer s.Close()
			var owner *tasks.RuntimeRecoveryFinalization
			dir, owner, _, v, prepared = retentionClientOwner(t, s.URL)
			defer owner.Close()
			if err := owner.WithPreparedReleaseSubmission(ctx, v.Binding, v.AuthorizationHash, func(g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
				if _, e := g.RetainHistory(ctx); e != nil {
					return e
				}
				var e error
				retained, e = g.PinRetentionProof(ctx)
				if e != nil {
					return e
				}
				wire, _ := json.Marshal(retained)
				h := recoveryReadHash(wire)
				if e = g.RecordRetentionReceipt(ctx, RecoveryRetentionReceipt{SchemaVersion: 1, Recorded: true, OperationID: retained.OperationID, RetentionHash: retained.RetentionHash, ProofHash: h, RequestHash: h}); e != nil {
					return e
				}
				_, e = g.ArchiveRetainedHistory(ctx)
				return e
			}); err != nil {
				t.Fatal(err)
			}
			if err := owner.WithSourceCleanupSubmission(ctx, v.Binding, v.AuthorizationHash, func(g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
				if _, e := g.CleanupArchivedSources(ctx); e != nil {
					return e
				}
				if _, e := g.PrepareCleanedCommit(ctx); e != nil {
					return e
				}
				var e error
				cleaned, e = g.PinCleanedCommitProof(ctx)
				if e != nil {
					return e
				}
				wire, _ := json.Marshal(cleaned)
				h := recoveryReadHash(wire)
				return g.RecordCleanedCommitReceipt(ctx, RecoveryCleanedCommitReceipt{SchemaVersion: 1, Recorded: true, OperationID: cleaned.OperationID, CleanedIntentHash: cleaned.CleanedIntentHash, ProofHash: h, RequestHash: h})
			}); err != nil {
				t.Fatal(err)
			}
			if err := owner.WithSourceCleanupSubmission(ctx, v.Binding, v.AuthorizationHash, func(g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
				_, e := g.ArchiveCleanedCommitHistory(ctx)
				return e
			}); err != nil {
				t.Fatal(err)
			}
			c := New(s.URL)
			result, receipt, err := c.ReleaseRecoveryAdmissionAndSubmitEvidence(ctx, "isolated-token", v, owner)
			if result.RemovedFiles != 5 || managerPosts.Load() < 1 {
				t.Fatal("release was not followed by bounded HTTP", result, err)
			}
			if mode == "recorded" {
				if err != nil || result.Outcome != tasks.RuntimeRecoveryAdmissionDurable || !receipt.Recorded || receipt.OperationID != prepared.OperationID {
					t.Fatal("Manager evidence was not confirmed", result, receipt, err)
				}
			} else if err != ErrRecoveryAdmissionReleaseUnconfirmed || result.Outcome != tasks.RuntimeRecoveryAdmissionUncertain || receipt != (RecoveryAdmissionReleaseReceipt{}) {
				t.Fatal("failed Manager submission concealed local release uncertainty", result, receipt, err)
			}
			if _, err := os.Lstat(filepath.Join(dir, "mutation-inflight.json")); !os.IsNotExist(err) {
				t.Fatal("post-release evidence submitted while fence remained")
			}
		})
	}
}
