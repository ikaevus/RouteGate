package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

var ErrRecoveryRetentionUnconfirmed = errors.New("recovery retained history storage was not confirmed")

type RecoveryRetentionReceipt = tasks.RuntimeRecoveryRetentionReceipt

// Dormant. Require history already retained by this original live owner. The
// existing readiness reads, pin, HTTP and durable ack share its mutex/flock and
// original deadline. Nothing consumes this storage acknowledgement as authority.
func (c *Client) SubmitRecoveryRetention(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (RecoveryRetentionReceipt, error) {
	var zero, result RecoveryRetentionReceipt
	_, e := c.withRecoveryFenceReleaseReadiness(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		p, e := g.PinRetentionProof(bounded)
		if e != nil {
			return ErrRecoveryRetentionUnconfirmed
		}
		result, e = c.submitRecoveryRetention(bounded, token, v, p, g.RetentionProof)
		if e != nil || g.RecordRetentionReceipt(bounded, result) != nil {
			return ErrRecoveryRetentionUnconfirmed
		}
		return nil
	})
	if e != nil {
		return zero, ErrRecoveryRetentionUnconfirmed
	}
	return result, nil
}

func (c *Client) submitRecoveryRetention(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, frozen tasks.RuntimeRecoveryRetentionProof, refresh func(context.Context) (tasks.RuntimeRecoveryRetentionProof, error)) (RecoveryRetentionReceipt, error) {
	var zero RecoveryRetentionReceipt
	wire, e := json.Marshal(frozen)
	if e != nil || refresh == nil {
		return zero, ErrRecoveryRetentionUnconfirmed
	}
	hash := recoveryReadHash(wire)
	check := func() bool {
		p, e := refresh(ctx)
		now := time.Now().UTC()
		return e == nil && ctx.Err() == nil && !now.Before(v.Authorization.IssuedAt) && now.Before(v.Authorization.ExpiresAt) && p == frozen && frozen.SchemaVersion == 1 && frozen.Purpose == tasks.RuntimeRecoveryRetentionPurpose && frozen.OperationID == v.Authorization.OperationID && frozen.AgentCredentialGeneration == v.Binding.AgentCredentialGeneration && frozen.AgentCredentialGeneration == v.Authorization.AgentCredentialGeneration && frozen.AuthorizationHash == v.AuthorizationHash && frozen.Observation == v.Authorization.Expected && !frozen.RetainedAt.Before(v.Authorization.IssuedAt) && !frozen.ObservedAt.Before(frozen.RetainedAt) && frozen.ObservedAt.Before(v.Authorization.ExpiresAt) && !frozen.ObservedAt.After(now)
	}
	valid := func(r RecoveryRetentionReceipt) bool {
		return r.SchemaVersion == 1 && r.Recorded && r.OperationID == frozen.OperationID && r.RetentionHash == frozen.RetentionHash && r.ProofHash == hash && r.RequestHash == hash
	}
	path := "/api/v1/agent/recovery-observations/" + url.PathEscape(frozen.OperationID) + "/retention"
	r, e := exchangePinnedRecoveryEvidence(ctx, c, token, path, wire, check, valid)
	if e != nil {
		return zero, ErrRecoveryRetentionUnconfirmed
	}
	return r, nil
}

// Current identity first, then exact historical HTTP readback under the original
// flock and complete source snapshots. No runtime capture, new POST, ack repair,
// owner resume or clock/authority renewal, including after expiry or restart.
func (c *Client) ReconcileRecoveryRetention(ctx context.Context, token, agentID, serverID, dir string) (RecoveryRetentionReceipt, error) {
	var zero, result RecoveryRetentionReceipt
	b, e := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if e != nil {
		return zero, ErrRecoveryRetentionUnconfirmed
	}
	e = tasks.WithHistoricalRecoveryRetention(ctx, dir, b, func(h tasks.RuntimeRecoveryRetentionHistory) error {
		wire, e := json.Marshal(h.Proof)
		if e != nil {
			return ErrRecoveryRetentionUnconfirmed
		}
		hash := recoveryReadHash(wire)
		path := "/api/v1/agent/recovery-observations/" + url.PathEscape(h.Proof.OperationID) + "/retention/verify"
		if c.recoveryRead(ctx, path, token, wire, &result) != nil || result.SchemaVersion != 1 || !result.Recorded || result.OperationID != h.Proof.OperationID || result.RetentionHash != h.Proof.RetentionHash || result.ProofHash != hash || result.RequestHash != hash || (h.LocalReceipt.SchemaVersion != 0 && result != h.LocalReceipt) {
			return ErrRecoveryRetentionUnconfirmed
		}
		return nil
	})
	if e != nil {
		return zero, ErrRecoveryRetentionUnconfirmed
	}
	return result, nil
}
