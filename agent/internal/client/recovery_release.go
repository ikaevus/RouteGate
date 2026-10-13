package client

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

type VerifiedRecoveryReleaseAuthorization struct {
	Binding           tasks.RuntimeRecoveryGrantBinding
	Authorization     tasks.RuntimeRecoveryReleaseAuthorization
	AuthorizationHash string
}
type recoveryReleaseRequest struct {
	recoveryGrantRequest
	GrantHash       string `json:"grantHash"`
	ConsumptionHash string `json:"consumptionHash"`
	CommitProofHash string `json:"commitProofHash"`
}
type recoveryReleaseReply struct {
	SchemaVersion             int                                       `json:"schemaVersion"`
	OperationID               string                                    `json:"operationId"`
	AgentID                   string                                    `json:"agentId"`
	ServerID                  string                                    `json:"serverId"`
	AgentCredentialGeneration int64                                     `json:"agentCredentialGeneration"`
	RequestHash               string                                    `json:"requestHash"`
	AuthorizationHash         string                                    `json:"authorizationHash"`
	Authorization             tasks.RuntimeRecoveryReleaseAuthorization `json:"authorization"`
}

// Dormant read-only delivery. Independently authenticate current generation,
// retain historical state under flock across HTTP and never resume an owner,
// consume authority, write files, apply configuration or release a fence.
func (c *Client) FetchRecoveryReleaseAuthorization(ctx context.Context, token, agentID, serverID, dir string) (VerifiedRecoveryReleaseAuthorization, error) {
	var zero, out VerifiedRecoveryReleaseAuthorization
	started := time.Now().UTC()
	b, err := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if err != nil {
		return zero, errRecoveryGrantRead
	}
	err = tasks.WithHistoricalRecoverySubmission(ctx, dir, b, func(p tasks.RuntimeRecoveryCommitProof) error {
		nonce, e := recoveryReadNonce()
		if e != nil {
			return e
		}
		proofWire, e := json.Marshal(p)
		if e != nil {
			return errRecoveryGrantRead
		}
		data, _ := json.Marshal(recoveryReleaseRequest{recoveryGrantRequest{1, p.OperationID, agentID, serverID, b.AgentCredentialGeneration, nonce}, p.GrantHash, p.ConsumptionHash, recoveryReadHash(proofWire)})
		var reply recoveryReleaseReply
		if c.recoveryRead(ctx, "/api/v1/agent/recovery-observations/"+url.PathEscape(p.OperationID)+"/release-authorization", token, data, &reply) != nil || ctx.Err() != nil || reply.SchemaVersion != 1 || reply.OperationID != p.OperationID || reply.AgentID != agentID || reply.ServerID != serverID || reply.AgentCredentialGeneration != b.AgentCredentialGeneration || reply.RequestHash != recoveryReadHash(data) {
			return errRecoveryGrantRead
		}
		wire, e := json.Marshal(reply.Authorization)
		if e != nil {
			return errRecoveryGrantRead
		}
		a, hash, e := tasks.ValidateRuntimeRecoveryReleaseAuthorization(wire, b, p, time.Now())
		if e != nil || hash != reply.AuthorizationHash {
			return errRecoveryGrantRead
		}
		out = VerifiedRecoveryReleaseAuthorization{b, a, hash}
		return nil
	})
	if err != nil || ctx.Err() != nil {
		return zero, errRecoveryGrantRead
	}
	// The callback's final file check may have taken time: expiry is final too.
	finished := time.Now().UTC()
	if finished.Before(started) || finished.Before(out.Authorization.IssuedAt) || !finished.Before(out.Authorization.ExpiresAt) {
		return zero, errRecoveryGrantRead
	}
	return out, nil
}
