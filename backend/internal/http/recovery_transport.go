package http

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/revocations"
)

type recoveryTransport struct{ service *revocations.Service }

func (t recoveryTransport) AcceptRecoveryTerminal(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	result, err := t.service.AcceptRecoveryTerminal(ctx, token, operation, data)
	if errors.Is(err, revocations.ErrPreflight) {
		return nil, agents.ErrObservationRejected
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
func (t recoveryTransport) VerifyRecoveryTerminal(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	result, err := t.service.VerifyRecoveryTerminal(ctx, token, operation, data)
	if errors.Is(err, revocations.ErrPreflight) {
		return nil, agents.ErrObservationRejected
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (t recoveryTransport) AcceptRecoveryFinalization(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	result, err := t.service.AcceptRecoveryFinalization(ctx, token, operation, data)
	if errors.Is(err, revocations.ErrPreflight) {
		return nil, agents.ErrObservationRejected
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
func (t recoveryTransport) VerifyRecoveryFinalization(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	result, err := t.service.VerifyRecoveryFinalization(ctx, token, operation, data)
	if errors.Is(err, revocations.ErrPreflight) {
		return nil, agents.ErrObservationRejected
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (t recoveryTransport) AcceptRecoveryCommit(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	result, err := t.service.AcceptRecoveryCommit(ctx, token, operation, data)
	if errors.Is(err, revocations.ErrPreflight) {
		return nil, agents.ErrObservationRejected
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
func (t recoveryTransport) VerifyRecoveryCommit(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	result, err := t.service.VerifyRecoveryCommit(ctx, token, operation, data)
	if errors.Is(err, revocations.ErrPreflight) {
		return nil, agents.ErrObservationRejected
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (t recoveryTransport) ReadRecoveryGrant(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	result, err := t.service.ReadRecoveryGrant(ctx, token, operation, data)
	if errors.Is(err, revocations.ErrPreflight) {
		return nil, agents.ErrObservationRejected
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (t recoveryTransport) ReadRecoveryReleaseAuthorization(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	result, err := t.service.ReadRecoveryReleaseAuthorization(ctx, token, operation, data)
	if errors.Is(err, revocations.ErrPreflight) {
		return nil, agents.ErrObservationRejected
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (t recoveryTransport) NextRecoveryChallenge(ctx context.Context, token string) (json.RawMessage, error) {
	c, err := t.service.NextRecoveryChallenge(ctx, token)
	if errors.Is(err, revocations.ErrPreflight) {
		return nil, agents.ErrObservationRejected
	}
	if err != nil || c == nil {
		return nil, err
	}
	return json.Marshal(c)
}
func (t recoveryTransport) AcceptRecoveryEvidence(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	ack, err := t.service.AcceptRecoveryEvidence(ctx, token, operation, data)
	if errors.Is(err, revocations.ErrPreflight) {
		return nil, agents.ErrObservationRejected
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(ack)
}
