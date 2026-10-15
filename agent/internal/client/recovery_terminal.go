package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

var ErrRecoveryTerminalUnconfirmed = errors.New("recovery terminal storage was not confirmed")

type RecoveryTerminalReceipt = tasks.RuntimeRecoveryTerminalReceipt

// Dormant: Runner does not invoke this. Caller retains original live owner and
// must defer Close. We never release, resume, clean up or renew authority.
func (c *Client) SubmitRecoveryTerminal(ctx context.Context, token string, verified VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (RecoveryTerminalReceipt, error) {
	var zero, result RecoveryTerminalReceipt
	wire, e := json.Marshal(verified.Authorization)
	if e != nil || token == "" || verified.Binding.ManagerURL != c.managerURL {
		return zero, ErrRecoveryTerminalUnconfirmed
	}
	hash := recoveryReadHash(wire)
	if hash != verified.AuthorizationHash {
		return zero, ErrRecoveryTerminalUnconfirmed
	}
	bounded, cancel := context.WithDeadline(ctx, verified.Authorization.ExpiresAt)
	defer cancel()
	e = owner.WithTerminalSubmission(bounded, verified.Binding, hash, func(g *tasks.RuntimeRecoveryTerminalGuard) error {
		frozen, e := g.PinProof(bounded)
		if e != nil {
			return ErrRecoveryTerminalUnconfirmed
		}
		result, e = c.submitRecoveryTerminal(bounded, token, verified, frozen, g.Proof)
		if e != nil {
			return e
		}
		if g.RecordReceipt(bounded, result) != nil {
			return ErrRecoveryTerminalUnconfirmed
		}
		return nil
	})
	if e != nil {
		return zero, ErrRecoveryTerminalUnconfirmed
	}
	return result, nil
}
func (c *Client) submitRecoveryTerminal(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, frozen tasks.RuntimeRecoveryTerminalProof, refresh func(context.Context) (tasks.RuntimeRecoveryTerminalProof, error)) (RecoveryTerminalReceipt, error) {
	var zero RecoveryTerminalReceipt
	wire, e := json.Marshal(frozen)
	if e != nil {
		return zero, ErrRecoveryTerminalUnconfirmed
	}
	hash := recoveryReadHash(wire)
	check := func() bool {
		p, e := refresh(ctx)
		now := time.Now().UTC()
		return e == nil && ctx.Err() == nil && !now.Before(v.Authorization.IssuedAt) && now.Before(v.Authorization.ExpiresAt) && p == frozen && frozen.SchemaVersion == 1 && frozen.Purpose == "runtime_mutation_retirement_evidence_v1" && frozen.OperationID == v.Authorization.OperationID && frozen.AgentCredentialGeneration == v.Binding.AgentCredentialGeneration && frozen.AgentCredentialGeneration == v.Authorization.AgentCredentialGeneration && frozen.AuthorizationHash == v.AuthorizationHash && frozen.Observation == v.Authorization.Expected && !frozen.ObservedAt.Before(v.Authorization.IssuedAt) && !frozen.ObservedAt.After(now)
	}
	valid := func(r RecoveryTerminalReceipt) bool {
		return r.SchemaVersion == 1 && r.Recorded && r.OperationID == frozen.OperationID && r.TerminalHash == frozen.TerminalHash && r.ProofHash == hash && r.RequestHash == hash
	}
	path := "/api/v1/agent/recovery-observations/" + url.PathEscape(frozen.OperationID) + "/terminal"
	r, e := exchangePinnedRecoveryEvidence(ctx, c, token, path, wire, check, valid)
	if e != nil {
		return zero, ErrRecoveryTerminalUnconfirmed
	}
	return r, nil
}
