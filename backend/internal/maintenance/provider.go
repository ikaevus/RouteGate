package maintenance

import (
	"context"
	"errors"
	"time"
)

var (
	ErrUnknownCategory      = errors.New("unknown maintenance category")
	ErrCategoryUnavailable  = errors.New("maintenance category unavailable")
	ErrInvalidMode          = errors.New("invalid maintenance mode")
	ErrEmptySelection       = errors.New("maintenance selection is empty")
	ErrPlanNotFound         = errors.New("maintenance plan not found")
	ErrPlanExpired          = errors.New("maintenance plan expired")
	ErrPlanNotExecutable    = errors.New("maintenance plan is not executable")
	ErrConfirmationRequired = errors.New("maintenance confirmation is invalid")
	ErrUnsafeOwnership      = errors.New("maintenance storage ownership is unsafe")
)

type CleanupResult struct {
	DeletedCount   int64
	ReclaimedBytes int64
}

type Provider interface {
	Category() Category
	Analyze(context.Context, time.Time) (PlanItem, error)
	Cleanup(context.Context, PlanItem) (CleanupResult, error)
	Verify(context.Context, PlanItem) (int64, error)
}

type inventoryProvider interface {
	Inventory(context.Context, time.Time) (PlanItem, error)
}

type unavailableProvider struct{ category Category }

func (p unavailableProvider) Category() Category { return p.category }
func (p unavailableProvider) Analyze(context.Context, time.Time) (PlanItem, error) {
	return PlanItem{CategoryID: p.category.ID, Scope: p.category.Scope, RetentionDays: p.category.RetentionDays}, nil
}
func (p unavailableProvider) Cleanup(context.Context, PlanItem) (CleanupResult, error) {
	return CleanupResult{}, ErrCategoryUnavailable
}
func (p unavailableProvider) Verify(context.Context, PlanItem) (int64, error) { return 0, nil }
