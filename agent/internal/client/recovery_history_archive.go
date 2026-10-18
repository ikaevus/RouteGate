package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

var ErrRecoveryHistoryArchiveUnconfirmed = errors.New("independent recovery history was not confirmed")

// Dormant backup only. Current readiness and exact retention storage readback
// span the same original live owner/lock. The private history never crosses HTTP.
func (c *Client) ArchiveRecoveryRetentionHistory(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (string, error) {
	var hash string
	e := c.withAcknowledgedRecoveryRetention(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		var err error
		hash, err = g.ArchiveRetainedHistory(bounded)
		return err
	})
	if e != nil {
		return "", ErrRecoveryHistoryArchiveUnconfirmed
	}
	return hash, nil
}

// Reuse the same current authority/storage checks under the original owner for
// archive publication and read-only source planning. No parallel transport.
func (c *Client) withAcknowledgedRecoveryRetention(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization, fn func(context.Context, *tasks.RuntimeRecoveryPreparedReleaseGuard) error) error {
	_, e := c.withRecoveryFenceReleaseReadiness(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		p, e := g.AcknowledgedRetentionProof(bounded)
		if e != nil {
			return ErrRecoveryHistoryArchiveUnconfirmed
		}
		wire, e := json.Marshal(p)
		if e != nil {
			return ErrRecoveryHistoryArchiveUnconfirmed
		}
		proofHash := recoveryReadHash(wire)
		path := "/api/v1/agent/recovery-observations/" + url.PathEscape(p.OperationID) + "/retention/verify"
		var receipt RecoveryRetentionReceipt
		if c.recoveryRead(bounded, path, token, wire, &receipt) != nil || !archiveRetentionReceiptMatches(receipt, p, proofHash) {
			return ErrRecoveryHistoryArchiveUnconfirmed
		}
		fresh, e := g.AcknowledgedRetentionProof(bounded)
		if e != nil || fresh != p {
			return ErrRecoveryHistoryArchiveUnconfirmed
		}
		return fn(bounded, g)
	})
	return e
}

func archiveRetentionReceiptMatches(r RecoveryRetentionReceipt, p tasks.RuntimeRecoveryRetentionProof, hash string) bool {
	return r.SchemaVersion == 1 && r.Recorded && r.OperationID == p.OperationID && r.RetentionHash == p.RetentionHash && r.ProofHash == hash && r.RequestHash == hash
}

type RecoveryArchivedRetention struct {
	Receipt      RecoveryRetentionReceipt
	ManifestHash string
}

// Authenticate current identity, lock the ORIGINAL state, and verify only the
// independent copy. No runtime capture, new evidence, receipt repair or resume.
func (c *Client) ReconcileArchivedRecoveryRetention(ctx context.Context, token, agentID, serverID, dir, operation string) (RecoveryArchivedRetention, error) {
	var zero, result RecoveryArchivedRetention
	b, e := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if e != nil {
		return zero, ErrRecoveryHistoryArchiveUnconfirmed
	}
	e = tasks.WithArchivedRecoveryRetention(ctx, dir, operation, b, func(h tasks.RuntimeRecoveryArchivedHistory) error {
		wire, e := json.Marshal(h.History.Proof)
		if e != nil {
			return ErrRecoveryHistoryArchiveUnconfirmed
		}
		path := "/api/v1/agent/recovery-observations/" + url.PathEscape(operation) + "/retention/verify"
		var receipt RecoveryRetentionReceipt
		if c.recoveryRead(ctx, path, token, wire, &receipt) != nil || !archiveRetentionReceiptMatches(receipt, h.History.Proof, recoveryReadHash(wire)) || receipt != h.History.LocalReceipt {
			return ErrRecoveryHistoryArchiveUnconfirmed
		}
		result = RecoveryArchivedRetention{receipt, h.ManifestHash}
		return nil
	})
	if e != nil {
		return zero, ErrRecoveryHistoryArchiveUnconfirmed
	}
	return result, nil
}
