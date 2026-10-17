package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

var ErrRecoveryReleaseNotReady = errors.New("recovery fence release readiness was not confirmed")

// A read-only snapshot, never a release token, renewed authority or permission
// to delete anything. A future consumer must perform its own locked validation.
type RecoveryFenceReleaseReadiness struct {
	PreparedReceipt   RecoveryPreparedReleaseReceipt
	AuthorizationHash string
}

// Dormant original live owner only. Reauthenticate current identity, verify
// prepared storage and reread the exact original conditional authorization
// while the original mutex/flock and durable snapshots span every exchange.
// No receipt write, nonce consumption, owner resume, configuration or release.
func (c *Client) CheckRecoveryFenceReleaseReadiness(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (RecoveryFenceReleaseReadiness, error) {
	return c.withRecoveryFenceReleaseReadiness(ctx, token, v, owner, nil)
}

// RetainRecoveryFenceReleaseHistory performs the same fresh authenticated reads
// while holding the original owner, then retains its full private audit bundle.
// The returned digest certifies retention only, never deletion or fence release.
func (c *Client) RetainRecoveryFenceReleaseHistory(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (string, error) {
	var hash string
	_, e := c.withRecoveryFenceReleaseReadiness(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		var err error
		hash, err = g.RetainHistory(bounded)
		return err
	})
	if e != nil {
		return "", ErrRecoveryReleaseNotReady
	}
	return hash, nil
}

func (c *Client) withRecoveryFenceReleaseReadiness(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization, retained func(context.Context, *tasks.RuntimeRecoveryPreparedReleaseGuard) error) (RecoveryFenceReleaseReadiness, error) {
	var zero, result RecoveryFenceReleaseReadiness
	started := time.Now().UTC()
	wire, e := json.Marshal(v.Authorization)
	if e != nil || token == "" || v.Binding.ManagerURL != c.managerURL || recoveryReadHash(wire) != v.AuthorizationHash {
		return zero, ErrRecoveryReleaseNotReady
	}
	bounded, cancel := context.WithDeadline(ctx, v.Authorization.ExpiresAt)
	defer cancel()
	e = owner.WithPreparedReleaseSubmission(bounded, v.Binding, v.AuthorizationHash, func(g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		if retained != nil && g.PinRetentionSources(bounded) != nil {
			return ErrRecoveryReleaseNotReady
		}
		p, e := g.AcknowledgedProof(bounded)
		if e != nil {
			return ErrRecoveryReleaseNotReady
		}
		result, e = c.checkRecoveryFenceReleaseReadiness(bounded, token, v, p, g.AcknowledgedProof)
		if e == nil && retained != nil {
			e = retained(bounded, g)
		}
		return e
	})
	finished := time.Now().UTC()
	if e != nil || bounded.Err() != nil || finished.Before(started) || finished.Before(v.Authorization.IssuedAt) || !finished.Before(v.Authorization.ExpiresAt) {
		return zero, ErrRecoveryReleaseNotReady
	}
	return result, nil
}

func (c *Client) checkRecoveryFenceReleaseReadiness(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, frozen tasks.RuntimeRecoveryPreparedReleaseProof, refresh func(context.Context) (tasks.RuntimeRecoveryPreparedReleaseProof, error)) (RecoveryFenceReleaseReadiness, error) {
	var zero RecoveryFenceReleaseReadiness
	started := time.Now().UTC()
	a := v.Authorization
	authorityWire, e := json.Marshal(a)
	if e != nil || refresh == nil || token == "" || v.Binding.ManagerURL != c.managerURL || recoveryReadHash(authorityWire) != v.AuthorizationHash {
		return zero, ErrRecoveryReleaseNotReady
	}
	check := func() bool {
		p, e := refresh(ctx)
		now := time.Now().UTC()
		return e == nil && ctx.Err() == nil && !now.Before(started) && !now.Before(a.IssuedAt) && now.Before(a.ExpiresAt) && p == frozen && p.SchemaVersion == 1 && p.Purpose == tasks.RuntimeRecoveryPreparedReleasePurpose && p.OperationID == a.OperationID && p.AgentCredentialGeneration == v.Binding.AgentCredentialGeneration && p.AgentCredentialGeneration == a.AgentCredentialGeneration && p.AuthorizationHash == v.AuthorizationHash && p.Observation == a.Expected && p.Observation.AgentID == v.Binding.AgentID && p.Observation.ServerID == v.Binding.ServerID && !p.PreparedAt.Before(a.IssuedAt) && !p.ObservedAt.Before(p.PreparedAt) && !p.ObservedAt.After(now) && p.ObservedAt.Before(a.ExpiresAt)
	}
	if !check() {
		return zero, ErrRecoveryReleaseNotReady
	}
	b, e := c.verifyRecoveryIdentity(ctx, token, v.Binding.AgentID, v.Binding.ServerID)
	if e != nil || b != v.Binding || !check() {
		return zero, ErrRecoveryReleaseNotReady
	}
	pin, e := json.Marshal(frozen)
	if e != nil {
		return zero, ErrRecoveryReleaseNotReady
	}
	hash := recoveryReadHash(pin)
	path := "/api/v1/agent/recovery-observations/" + url.PathEscape(a.OperationID)
	var receipt RecoveryPreparedReleaseReceipt
	if c.recoveryRead(ctx, path+"/prepared-release/verify", token, pin, &receipt) != nil || receipt.SchemaVersion != 1 || !receipt.Recorded || receipt.OperationID != a.OperationID || receipt.ReleaseIntentHash != frozen.ReleaseIntentHash || receipt.ProofHash != hash || receipt.RequestHash != hash || !check() {
		return zero, ErrRecoveryReleaseNotReady
	}
	nonce, e := recoveryReadNonce()
	if e != nil {
		return zero, ErrRecoveryReleaseNotReady
	}
	request, e := json.Marshal(recoveryReleaseRequest{recoveryGrantRequest{1, a.OperationID, b.AgentID, b.ServerID, b.AgentCredentialGeneration, nonce}, a.GrantHash, a.ConsumptionHash, a.CommitProofHash})
	if e != nil || !check() {
		return zero, ErrRecoveryReleaseNotReady
	}
	var reply recoveryReleaseReply
	if c.recoveryRead(ctx, path+"/release-authorization", token, request, &reply) != nil || reply.SchemaVersion != 1 || reply.OperationID != a.OperationID || reply.AgentID != b.AgentID || reply.ServerID != b.ServerID || reply.AgentCredentialGeneration != b.AgentCredentialGeneration || reply.RequestHash != recoveryReadHash(request) || reply.AuthorizationHash != v.AuthorizationHash {
		return zero, ErrRecoveryReleaseNotReady
	}
	received, e := json.Marshal(reply.Authorization)
	if e != nil || !bytes.Equal(received, authorityWire) || !check() {
		return zero, ErrRecoveryReleaseNotReady
	}
	return RecoveryFenceReleaseReadiness{receipt, v.AuthorizationHash}, nil
}
