package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

var ErrRecoveryAdmissionReleaseUnconfirmed = errors.New("recovery admission-release evidence was not confirmed")

// A Manager storage receipt, not proof of VPN traffic cessation or permission
// to complete a reservation. No request or response contains a bearer secret.
type RecoveryAdmissionReleaseReceipt struct {
	SchemaVersion int    `json:"schemaVersion"`
	Recorded      bool   `json:"recorded"`
	OperationID   string `json:"operationId"`
	ProofHash     string `json:"proofHash"`
	RequestHash   string `json:"requestHash"`
}

func validAdmissionReleaseReceipt(r RecoveryAdmissionReleaseReceipt, p tasks.RuntimeRecoveryAdmissionReleaseProof, hash string) bool {
	return r.SchemaVersion == 1 && r.Recorded && r.OperationID == p.OperationID &&
		r.ProofHash == hash && r.RequestHash == hash
}

func (c *Client) readRecoveryAdmissionReleaseReceipt(ctx context.Context, token string, p tasks.RuntimeRecoveryAdmissionReleaseProof) (RecoveryAdmissionReleaseReceipt, error) {
	var receipt RecoveryAdmissionReleaseReceipt
	wire, err := json.Marshal(p)
	if err != nil {
		return receipt, ErrRecoveryAdmissionReleaseUnconfirmed
	}
	path := "/api/v1/agent/recovery-observations/" + url.PathEscape(p.OperationID) + "/admission-release/verify"
	if c.recoveryRead(ctx, path, token, wire, &receipt) != nil || !validAdmissionReleaseReceipt(receipt, p, recoveryReadHash(wire)) {
		return RecoveryAdmissionReleaseReceipt{}, ErrRecoveryAdmissionReleaseUnconfirmed
	}
	return receipt, nil
}

// Historical reconciliation authenticates the CURRENT Agent credential first,
// then holds the ORIGINAL admission flock across the immutable local history
// check and exact Manager readback. It never uploads new proof, performs a
// local release, recovers an owner, renews authority or touches VPN runtime.
// A matching receipt means only that Manager retained this exact Agent claim.
func (c *Client) ReconcileRecoveryAdmissionRelease(ctx context.Context, token, agentID, serverID, dir, operation string) (RecoveryAdmissionReleaseReceipt, error) {
	var zero, result RecoveryAdmissionReleaseReceipt
	b, err := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if err != nil {
		return zero, ErrRecoveryAdmissionReleaseUnconfirmed
	}
	err = tasks.WithRecoveryAdmissionReleaseHistory(ctx, dir, operation, b, func(p tasks.RuntimeRecoveryAdmissionReleaseProof) error {
		var e error
		result, e = c.readRecoveryAdmissionReleaseReceipt(ctx, token, p)
		return e
	})
	if err != nil {
		return zero, ErrRecoveryAdmissionReleaseUnconfirmed
	}
	return result, nil
}
