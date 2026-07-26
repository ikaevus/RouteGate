package agents

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// The existing handler fake models the legacy config-only repository. These
// methods opt it into the new optional operation transport while preserving
// the expected fallback to config tasks.
func (f *fakeAgentAPIRepository) ClaimNextAgentOperationTask(context.Context, string) (*AgentConfigTask, error) {
	return nil, nil
}

func (f *fakeAgentAPIRepository) CompleteAgentOperationTask(context.Context, CompleteAgentOperationJobInput) error {
	return pgx.ErrNoRows
}
