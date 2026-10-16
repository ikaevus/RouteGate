package client

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

// Dormant historical storage readback only. Current identity/generation is
// independently verified; private pinned history stays under flock through
// /terminal/verify. Never POST new evidence, retry mutation, write receipt,
// renew authority, capture runtime, resume owner or release Agent/Manager state.
func (c *Client) ReconcileRecoveryTerminal(ctx context.Context, token, agentID, serverID, dir string) (RecoveryTerminalReceipt, error) {
	var zero, result RecoveryTerminalReceipt
	b, err := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if err != nil {
		return zero, ErrRecoveryTerminalUnconfirmed
	}
	err = tasks.WithHistoricalRecoveryTerminal(ctx, dir, b, func(p tasks.RuntimeRecoveryTerminalProof) error {
		var e error
		result, e = c.verifyHistoricalRecoveryTerminal(ctx, token, p)
		return e
	})
	if err != nil {
		return zero, ErrRecoveryTerminalUnconfirmed
	}
	return result, nil
}

// Shared strict read-only transport. Caller keeps its validated filesystem
// snapshot locked through HTTP and revalidates it before exposing this receipt.
func (c *Client) verifyHistoricalRecoveryTerminal(ctx context.Context, token string, p tasks.RuntimeRecoveryTerminalProof) (RecoveryTerminalReceipt, error) {
	var zero, result RecoveryTerminalReceipt
	wire, e := json.Marshal(p)
	if e != nil {
		return zero, ErrRecoveryTerminalUnconfirmed
	}
	path := "/api/v1/agent/recovery-observations/" + url.PathEscape(p.OperationID) + "/terminal/verify"
	if c.recoveryRead(ctx, path, token, wire, &result) != nil || result.SchemaVersion != 1 || !result.Recorded || result.OperationID != p.OperationID || result.TerminalHash != p.TerminalHash || result.ProofHash != recoveryReadHash(wire) || result.RequestHash != result.ProofHash {
		return zero, ErrRecoveryTerminalUnconfirmed
	}
	return result, nil
}
