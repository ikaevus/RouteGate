package client

import (
	"context"
	"time"
)

// Shared strict recoveryExchange and bounded exact-byte retry/readback policy.
func exchangePinnedRecoveryEvidence[T any](ctx context.Context, c *Client, token, path string, wire []byte, check func() bool, valid func(T) bool) (T, error) {
	var zero T
	for attempt := 0; attempt < completeTaskMaxAttempts; attempt++ {
		if !check() {
			return zero, ErrRecoveryCommitUnconfirmed
		}
		var receipt T
		status, e := c.recoveryExchange(ctx, path, token, wire, &receipt)
		if !check() {
			return zero, ErrRecoveryCommitUnconfirmed
		}
		if e == nil && valid(receipt) {
			return receipt, nil
		}
		// Even a definitive rejection may follow an earlier uncertain attempt.
		// Readback is exact historical storage; it never repairs local drift.
		receipt = zero
		verifyStatus, verifyErr := c.recoveryExchange(ctx, path+"/verify", token, wire, &receipt)
		if !check() {
			return zero, ErrRecoveryCommitUnconfirmed
		}
		if verifyErr == nil && valid(receipt) {
			return receipt, nil
		}
		if (status >= 300 && status < 500 && status != 408 && status != 429) || (verifyStatus >= 300 && verifyStatus < 500 && verifyStatus != 409 && verifyStatus != 408 && verifyStatus != 429) {
			return zero, ErrRecoveryCommitUnconfirmed
		}
		if attempt+1 < completeTaskMaxAttempts {
			timer := time.NewTimer(completeTaskRetryBaseWait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return zero, ErrRecoveryCommitUnconfirmed
			case <-timer.C:
			}
		}
	}
	return zero, ErrRecoveryCommitUnconfirmed
}
