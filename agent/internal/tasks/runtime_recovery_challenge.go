package tasks

import (
	"context"
	"encoding/json"
	"time"
)

const runtimeRecoveryChallengeLifetime = 2 * time.Minute

// RuntimeRecoveryChallenge is an internal observation request, NOT an unlock
// grant. A future authenticated Manager transport must issue a random 256-bit
// nonce, reserve the node and persist the exact request and operator approval.
// This collector neither authenticates the caller nor consumes the nonce.
type RuntimeRecoveryChallenge struct {
	SchemaVersion int                        `json:"schemaVersion"`
	OperationID   string                     `json:"operationId"`
	Nonce         string                     `json:"nonce"`
	IssuedAt      time.Time                  `json:"issuedAt"`
	ExpiresAt     time.Time                  `json:"expiresAt"`
	Expected      RuntimeRecoveryObservation `json:"expected"`
}

// RuntimeRecoveryChallengeResponse binds a fresh read-only observation to the
// complete typed request (SHA-256 of encoding/json serialization), not to raw
// transport JSON. Neither this response nor its digest authorizes fence removal.
type RuntimeRecoveryChallengeResponse struct {
	SchemaVersion int                        `json:"schemaVersion"`
	OperationID   string                     `json:"operationId"`
	ChallengeHash string                     `json:"challengeHash"`
	ObservedAt    time.Time                  `json:"observedAt"`
	Observation   RuntimeRecoveryObservation `json:"observation"`
}

func (c RuntimeRecoveryChallenge) valid(binding RuntimeResultBinding, now time.Time) bool {
	o := c.Expected
	return c.SchemaVersion == 1 && canonicalTaskIDPattern.MatchString(c.OperationID) &&
		validRemovalHash(c.Nonce) && !c.IssuedAt.IsZero() && !c.ExpiresAt.IsZero() &&
		!now.Before(c.IssuedAt) && now.Before(c.ExpiresAt) &&
		c.ExpiresAt.After(c.IssuedAt) && c.ExpiresAt.Sub(c.IssuedAt) <= runtimeRecoveryChallengeLifetime &&
		binding.ManagerURL != "" && canonicalTaskIDPattern.MatchString(binding.AgentID) &&
		canonicalTaskIDPattern.MatchString(binding.ServerID) &&
		o.SchemaVersion == 1 && canonicalTaskIDPattern.MatchString(o.TaskID) &&
		o.AgentID == binding.AgentID && o.ServerID == binding.ServerID &&
		validRemovalHash(o.MarkerHash) && validRemovalHash(o.ReceiptHash) &&
		validRemovalHash(o.EnvelopeHash) && validRuntimeWitness(o.Witness)
}

// ObserveRuntimeRecoveryChallenge reuses the locked observer and requires exact
// equality with the Manager's expected evidence. It has no dispatch/HTTP/CLI
// entry point. Clock disagreement fails closed; there is no clock-skew grace.
// A subsequent commit MUST independently re-observe under its own held lock;
// this returned snapshot cannot eliminate a later state-change race.
func ObserveRuntimeRecoveryChallenge(ctx context.Context, dir string, binding RuntimeResultBinding, challenge RuntimeRecoveryChallenge, activePath string, adapter VPNCoreAdapter) (RuntimeRecoveryChallengeResponse, error) {
	return observeRuntimeRecoveryChallenge(ctx, dir, binding, challenge, activePath, adapter, time.Now)
}

func observeRuntimeRecoveryChallenge(ctx context.Context, dir string, binding RuntimeResultBinding, challenge RuntimeRecoveryChallenge, activePath string, adapter VPNCoreAdapter, now func() time.Time) (RuntimeRecoveryChallengeResponse, error) {
	deny := func() (RuntimeRecoveryChallengeResponse, error) {
		return RuntimeRecoveryChallengeResponse{}, ErrRuntimeMutationBlocked
	}
	started := now()
	if ctx.Err() != nil || !challenge.valid(binding, started) {
		return deny()
	}
	bounded, cancel := context.WithDeadline(ctx, challenge.ExpiresAt)
	defer cancel()
	observation, err := ObserveRuntimeRecovery(bounded, dir, binding, challenge.Expected.TaskID, activePath, adapter)
	finished := now()
	if err != nil || bounded.Err() != nil || finished.Before(started) || !challenge.valid(binding, finished) || observation != challenge.Expected {
		return deny()
	}
	data, err := json.Marshal(challenge)
	if err != nil {
		return deny()
	}
	return RuntimeRecoveryChallengeResponse{SchemaVersion: 1, OperationID: challenge.OperationID, ChallengeHash: markerDigest(data), ObservedAt: finished.UTC(), Observation: observation}, nil
}
