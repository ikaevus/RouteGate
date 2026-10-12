package heartbeat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func (r *Runner) processRecoveryObservation(ctx context.Context, c tasks.RuntimeRecoveryChallenge) error {
	if !r.runtimeMutationManagerReady || !r.cfg.ServiceControlEnabled || !tasks.SupportsRuntimeRecovery(r.vpnCoreAdapter) || c.Expected.AgentID != r.cfg.AgentID || c.Expected.ServerID != r.cfg.ServerID || time.Now().Before(c.IssuedAt) || !time.Now().Before(c.ExpiresAt) {
		return tasks.ErrRuntimeMutationBlocked
	}
	data, err := json.Marshal(c)
	if err != nil {
		return tasks.ErrRuntimeMutationBlocked
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if r.recoveryResponse == nil || r.recoveryResponse.ChallengeHash != hash {
		dir := r.runtimeMutationDir
		if dir == "" {
			dir = tasks.DefaultRuntimeMutationDir
		}
		response, err := tasks.ObserveRuntimeRecoveryChallenge(ctx, dir, r.runtimeResultBinding(), c, r.cfg.ActiveConfigPath, r.vpnCoreAdapter)
		if err != nil {
			return tasks.ErrRuntimeMutationBlocked
		}
		r.recoveryResponse = &response
	}
	// Memory-only retry cache. A restart may re-observe, but Manager never
	// overwrites accepted evidence. No local durable state or fence is changed.
	return r.client.SubmitRecoveryEvidence(ctx, r.cfg.AgentToken, c, *r.recoveryResponse)
}
