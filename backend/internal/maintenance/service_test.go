package maintenance

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

type fakePlanRepository struct {
	plan      Plan
	tokenHash []byte
}

func (r *fakePlanRepository) Create(_ context.Context, plan Plan, tokenHash []byte) (Plan, error) {
	plan.ID = "11111111-1111-4111-8111-111111111111"
	plan.Status = StatusPlanned
	plan.CreatedAt = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	r.plan = plan
	r.tokenHash = append([]byte(nil), tokenHash...)
	return plan, nil
}
func (r *fakePlanRepository) Get(context.Context, string) (Plan, error) { return r.plan, nil }
func (r *fakePlanRepository) Acquire(_ context.Context, _ string, userID string, tokenHash []byte, now time.Time) (Plan, error) {
	if r.plan.Status != StatusPlanned || r.plan.CreatedByUserID != userID {
		return Plan{}, ErrPlanNotExecutable
	}
	if !now.Before(r.plan.ExpiresAt) {
		return Plan{}, ErrPlanExpired
	}
	if !bytes.Equal(r.tokenHash, tokenHash) {
		return Plan{}, ErrConfirmationRequired
	}
	r.plan.Status = StatusRunning
	r.plan.StartedAt = &now
	return r.plan, nil
}
func (r *fakePlanRepository) Complete(_ context.Context, _ string, report Report) (Plan, error) {
	r.plan.Status = StatusSucceeded
	r.plan.Report = &report
	return r.plan, nil
}
func (r *fakePlanRepository) Fail(_ context.Context, _ string, report Report, code string) (Plan, error) {
	r.plan.Status = StatusFailed
	r.plan.ErrorCode = code
	r.plan.Report = &report
	return r.plan, nil
}

type fakeProvider struct {
	category   Category
	candidates int64
	cleanupErr error
	cleaned    bool
}

func (p *fakeProvider) Category() Category { return p.category }
func (p *fakeProvider) Analyze(_ context.Context, cutoff time.Time) (PlanItem, error) {
	return PlanItem{CategoryID: p.category.ID, Scope: p.category.Scope, RetentionDays: p.category.RetentionDays, Cutoff: cutoff, CandidateCount: p.candidates}, nil
}
func (p *fakeProvider) Cleanup(context.Context, PlanItem) (CleanupResult, error) {
	if p.cleanupErr != nil {
		return CleanupResult{}, p.cleanupErr
	}
	p.cleaned = true
	return CleanupResult{DeletedCount: p.candidates}, nil
}
func (p *fakeProvider) Verify(context.Context, PlanItem) (int64, error) {
	if p.cleaned {
		return 0, nil
	}
	return p.candidates, nil
}

func fixedNow() time.Time { return time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC) }

func TestRecommendedPlanSelectsOnlyAvailableRecommendedProviders(t *testing.T) {
	repo := &fakePlanRepository{}
	recommended := &fakeProvider{category: Category{ID: "recommended", Scope: "postgresql", Recommended: true, Selectable: true, RetentionDays: 7}, candidates: 3}
	advanced := &fakeProvider{category: Category{ID: "advanced", Scope: "postgresql", Selectable: true, RetentionDays: 90}, candidates: 4}
	unavailable := unavailableProvider{Category{ID: "future", Scope: "agent", Recommended: false, Selectable: false}}
	service := newService(repo, nil, []Provider{recommended, advanced, unavailable}, fixedNow)

	response, err := service.CreatePlan(context.Background(), "user-1", CreatePlanRequest{Mode: ModeRecommended})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Plan.Payload.Items) != 1 || response.Plan.Payload.Items[0].CategoryID != "recommended" {
		t.Fatalf("unexpected recommended plan: %#v", response.Plan.Payload.Items)
	}
	if response.ConfirmationToken == "" || bytes.Contains(repo.tokenHash, []byte(response.ConfirmationToken)) {
		t.Fatal("confirmation token must be returned once and stored only as a hash")
	}
	if got := response.Plan.Payload.Items[0].Cutoff; !got.Equal(fixedNow().Add(-7 * 24 * time.Hour)) {
		t.Fatalf("cutoff=%s", got)
	}
}

func TestAdvancedPlanRejectsUnavailableCategory(t *testing.T) {
	service := newService(&fakePlanRepository{}, nil, []Provider{
		unavailableProvider{Category{ID: "future", Scope: "agent", Selectable: false}},
	}, fixedNow)
	_, err := service.CreatePlan(context.Background(), "user-1", CreatePlanRequest{Mode: ModeAdvanced, Categories: []string{"future"}})
	if !errors.Is(err, ErrCategoryUnavailable) {
		t.Fatalf("error=%v", err)
	}
}

func TestPlanExecutesOnceWithReturnedConfirmationAndVerifiesProvider(t *testing.T) {
	repo := &fakePlanRepository{}
	provider := &fakeProvider{category: Category{ID: "safe", Scope: "postgresql", Recommended: true, Selectable: true, RetentionDays: 7}, candidates: 8}
	service := newService(repo, nil, []Provider{provider}, fixedNow)
	created, err := service.CreatePlan(context.Background(), "user-1", CreatePlanRequest{Mode: ModeRecommended})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Execute(context.Background(), created.Plan.ID, "user-1", created.ConfirmationToken)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusSucceeded || plan.Report == nil || plan.Report.Providers[0].DeletedCount != 8 {
		t.Fatalf("unexpected report: %#v", plan)
	}
	if _, err := service.Execute(context.Background(), created.Plan.ID, "user-1", created.ConfirmationToken); !errors.Is(err, ErrPlanNotExecutable) {
		t.Fatalf("second execution error=%v", err)
	}
}

func TestWrongConfirmationDoesNotStartCleanup(t *testing.T) {
	repo := &fakePlanRepository{}
	provider := &fakeProvider{category: Category{ID: "safe", Scope: "postgresql", Recommended: true, Selectable: true, RetentionDays: 7}, candidates: 2}
	service := newService(repo, nil, []Provider{provider}, fixedNow)
	created, err := service.CreatePlan(context.Background(), "user-1", CreatePlanRequest{Mode: ModeRecommended})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(context.Background(), created.Plan.ID, "user-1", "wrong"); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("error=%v", err)
	}
	if provider.cleaned {
		t.Fatal("provider ran without valid confirmation")
	}
}

func TestProviderFailureProducesFailedReport(t *testing.T) {
	repo := &fakePlanRepository{}
	provider := &fakeProvider{category: Category{ID: "safe", Scope: "manager", Recommended: true, Selectable: true, RetentionDays: 1}, candidates: 1, cleanupErr: ErrUnsafeOwnership}
	service := newService(repo, nil, []Provider{provider}, fixedNow)
	created, err := service.CreatePlan(context.Background(), "user-1", CreatePlanRequest{Mode: ModeRecommended})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Execute(context.Background(), created.Plan.ID, "user-1", created.ConfirmationToken)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusFailed || plan.Report.Providers[0].ErrorCode != "unsafe_ownership" {
		t.Fatalf("unexpected failed report: %#v", plan)
	}
}
