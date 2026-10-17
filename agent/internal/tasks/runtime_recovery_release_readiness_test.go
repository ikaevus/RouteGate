package tasks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReleaseReadinessAcknowledgedOwner(t *testing.T) {
	for _, mode := range []string{"ready", "missing_pin", "missing_ack", "runtime", "expired", "ack_tamper", "ack_replaced", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			_, a, b, _, m := preparedSubmissionFixture(t)
			defer m.Close()
			var frozen RuntimeRecoveryPreparedReleaseProof
			if mode != "missing_pin" {
				if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
					var err error
					frozen, err = g.PinProof(context.Background())
					if err != nil {
						return err
					}
					if mode == "missing_ack" {
						return nil
					}
					return g.RecordReceipt(context.Background(), receiptForPreparedProof(frozen))
				}); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "runtime" {
				a.generation.PID++
			}
			if mode == "expired" {
				m.now = func() time.Time { return m.authorization.ExpiresAt }
			}
			if mode == "cancelled" {
				cancel()
			}
			if mode == "ack_tamper" {
				if err := os.WriteFile(filepath.Join(m.dir, runtimeRecoveryPreparedAcknowledgementFile), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := terminalHistoryTree(t, m.dir)
			var escaped *RuntimeRecoveryPreparedReleaseGuard
			var copied RuntimeRecoveryPreparedReleaseGuard
			err := m.WithPreparedReleaseSubmission(ctx, b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				if mode == "ack_replaced" {
					data, err := os.ReadFile(filepath.Join(m.dir, runtimeRecoveryPreparedAcknowledgementFile))
					if err != nil {
						t.Fatal(err)
					}
					tmp := filepath.Join(m.dir, ".replacement")
					if err := os.WriteFile(tmp, data, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(tmp, filepath.Join(m.dir, runtimeRecoveryPreparedAcknowledgementFile)); err != nil {
						t.Fatal(err)
					}
				}
				escaped = g
				copied = *g
				p, err := g.AcknowledgedProof(ctx)
				if mode == "ready" && p != frozen {
					t.Error("proof renewed")
				}
				return err
			})
			if (err == nil) != (mode == "ready") {
				t.Fatal("unexpected readiness", err)
			}
			for _, g := range []*RuntimeRecoveryPreparedReleaseGuard{escaped, &copied, nil} {
				if p, err := g.AcknowledgedProof(context.Background()); err != ErrRuntimeMutationBlocked || p != (RuntimeRecoveryPreparedReleaseProof{}) {
					t.Fatal("escaped readiness")
				}
			}
			assertTerminalHistoryTree(t, m.dir, before)
		})
	}
}
