package tasks

import "context"

// Read-only historical terminal proof under the original admission flock.
// Independently authenticate binding with current credentials first. A durable
// pin is required; missing acknowledgement is allowed for uncertain HTTP/crash.
// Expired authority remains history. Never capture runtime, create a proof,
// persist a receipt, resume ownership, complete cleanup or release any fence.
func WithHistoricalRecoveryTerminal(ctx context.Context, dir string, b RuntimeRecoveryGrantBinding, fn func(RuntimeRecoveryTerminalProof) error) error {
	if ctx.Err() != nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	lock, err := lockRuntimeMutationExisting(dir)
	if err != nil {
		return ErrRuntimeMutationBlocked
	}
	defer lock.Close()
	return withTerminalRecoveryHistoryAndConfirmationLocked(ctx, dir, b, lock, func(_ RuntimeRecoveryFinalizationProof, p *RuntimeRecoveryTerminalProof) error {
		if p == nil {
			return ErrRuntimeMutationBlocked
		}
		return fn(*p)
	})
}
