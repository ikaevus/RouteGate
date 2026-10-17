package tasks

import "context"

// AcknowledgedProof requires both files published by this original live owner.
// Historical receipts, including successful readback, cannot be adopted here.
// Like Proof, this method is valid only inside the synchronous owner callback.
func (g *RuntimeRecoveryPreparedReleaseGuard) AcknowledgedProof(ctx context.Context) (RuntimeRecoveryPreparedReleaseProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil || m.preparedSubmission == nil || m.preparedAcknowledgement == nil {
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	return m.preparedReleaseProofLocked(ctx)
}
