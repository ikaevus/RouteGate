package maintenance

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
)

const planTTL = 15 * time.Minute

type auditRecorder interface {
	RecordSafe(context.Context, audit.EventInput)
}

type Service struct {
	repo      planRepository
	audit     auditRecorder
	providers []Provider
	now       func() time.Time
}

func NewService(pool *pgxpool.Pool, recorder auditRecorder) *Service {
	providers := databaseProviders(pool)
	providers = append(providers,
		managerPartialProvider(),
		unavailableProvider{Category{ID: "raw_traffic_history", Scope: "postgresql", BlockedReason: "rollup_archive_boundary_required"}},
		unavailableProvider{Category{ID: "platform_rollback_backups", Scope: "manager", BlockedReason: "privileged_cleanup_contract_required"}},
		unavailableProvider{Category{ID: "agent_runtime_artifacts", Scope: "agent", BlockedReason: "agent_cleanup_contract_required"}},
		unavailableProvider{Category{ID: "prometheus_tsdb_retention", Scope: "prometheus", BlockedReason: "retention_configuration_only"}},
	)
	return &Service{repo: NewRepository(pool), audit: recorder, providers: providers, now: time.Now}
}

func newService(repo planRepository, recorder auditRecorder, providers []Provider, now func() time.Time) *Service {
	return &Service{repo: repo, audit: recorder, providers: providers, now: now}
}

func (s *Service) Inventory(ctx context.Context) InventoryResponse {
	now := s.now().UTC()
	response := InventoryResponse{AnalyzedAt: now, Categories: make([]Analysis, 0, len(s.providers))}
	for _, provider := range s.providers {
		category := provider.Category()
		analysis := Analysis{Category: category}
		if !category.Selectable {
			response.Categories = append(response.Categories, analysis)
			continue
		}
		item, err := provider.Analyze(ctx, cutoffFor(now, category.RetentionDays))
		if err != nil {
			analysis.Selectable = false
			analysis.BlockedReason = providerErrorCode(err, "analysis_failed")
		} else {
			analysis.CandidateCount = item.CandidateCount
			analysis.EstimatedBytes = item.EstimatedBytes
		}
		response.Categories = append(response.Categories, analysis)
	}
	return response
}

func (s *Service) CreatePlan(ctx context.Context, userID string, request CreatePlanRequest) (CreatePlanResponse, error) {
	if request.Mode != ModeRecommended && request.Mode != ModeAdvanced {
		return CreatePlanResponse{}, ErrInvalidMode
	}
	selected, err := s.selectProviders(request)
	if err != nil {
		return CreatePlanResponse{}, err
	}
	now := s.now().UTC()
	items := make([]PlanItem, 0, len(selected))
	categories := make([]string, 0, len(selected))
	for _, provider := range selected {
		category := provider.Category()
		item, err := provider.Analyze(ctx, cutoffFor(now, category.RetentionDays))
		if err != nil {
			return CreatePlanResponse{}, fmt.Errorf("%w: %s", ErrCategoryUnavailable, category.ID)
		}
		items = append(items, item)
		categories = append(categories, category.ID)
	}
	token, tokenHash, err := newConfirmationToken()
	if err != nil {
		return CreatePlanResponse{}, err
	}
	plan, err := s.repo.Create(ctx, Plan{
		Mode: request.Mode, SelectedCategories: categories,
		Payload:         PlanPayload{SchemaVersion: 1, Items: items},
		CreatedByUserID: userID, ExpiresAt: now.Add(planTTL),
	}, tokenHash)
	if err != nil {
		return CreatePlanResponse{}, err
	}
	s.record(ctx, userID, "maintenance.cleanup.planned", plan.ID, audit.ResultSuccess, map[string]any{
		"mode": request.Mode, "categories": categories, "candidate_count": totalCandidates(items),
	})
	return CreatePlanResponse{Plan: plan, ConfirmationToken: token}, nil
}

func (s *Service) GetPlan(ctx context.Context, id string) (Plan, error) { return s.repo.Get(ctx, id) }

func (s *Service) Execute(ctx context.Context, id, userID, confirmationToken string) (Plan, error) {
	tokenHash := sha256.Sum256([]byte(confirmationToken))
	started := s.now().UTC()
	plan, err := s.repo.Acquire(ctx, id, userID, tokenHash[:], started)
	if err != nil {
		return Plan{}, err
	}
	report := Report{SchemaVersion: 1, Status: StatusSucceeded, StartedAt: started}
	providers := s.providerMap()
	for _, item := range plan.Payload.Items {
		provider, ok := providers[item.CategoryID]
		if !ok || !provider.Category().Selectable {
			report.Status = StatusFailed
			report.Providers = append(report.Providers, ProviderReport{CategoryID: item.CategoryID, Status: StatusFailed, ErrorCode: "provider_unavailable"})
			break
		}
		result, cleanupErr := provider.Cleanup(ctx, item)
		providerReport := ProviderReport{CategoryID: item.CategoryID, DeletedCount: result.DeletedCount, ReclaimedBytes: result.ReclaimedBytes}
		if cleanupErr != nil {
			report.Status = StatusFailed
			providerReport.Status = StatusFailed
			providerReport.ErrorCode = providerErrorCode(cleanupErr, "cleanup_failed")
			report.Providers = append(report.Providers, providerReport)
			break
		}
		remaining, verifyErr := provider.Verify(ctx, item)
		providerReport.RemainingCount = remaining
		if verifyErr != nil || remaining != 0 {
			report.Status = StatusFailed
			providerReport.Status = StatusFailed
			providerReport.ErrorCode = providerErrorCode(verifyErr, "verification_failed")
		} else {
			providerReport.Status = StatusSucceeded
		}
		report.Providers = append(report.Providers, providerReport)
		if providerReport.Status == StatusFailed {
			break
		}
	}
	report.CompletedAt = s.now().UTC()
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if report.Status == StatusFailed {
		plan, err = s.repo.Fail(finalizeCtx, id, report, "provider_failed")
		if err == nil {
			s.record(finalizeCtx, userID, "maintenance.cleanup.failed", id, audit.ResultFailure, reportMetadata(report))
		}
		return plan, err
	}
	plan, err = s.repo.Complete(finalizeCtx, id, report)
	if err == nil {
		s.record(finalizeCtx, userID, "maintenance.cleanup.completed", id, audit.ResultSuccess, reportMetadata(report))
	}
	return plan, err
}

func (s *Service) selectProviders(request CreatePlanRequest) ([]Provider, error) {
	requested := map[string]struct{}{}
	for _, id := range request.Categories {
		if _, duplicate := requested[id]; duplicate {
			continue
		}
		requested[id] = struct{}{}
	}
	var selected []Provider
	for _, provider := range s.providers {
		category := provider.Category()
		choose := request.Mode == ModeRecommended && category.Recommended
		if request.Mode == ModeAdvanced {
			_, choose = requested[category.ID]
		}
		if !choose {
			continue
		}
		if !category.Selectable {
			return nil, fmt.Errorf("%w: %s", ErrCategoryUnavailable, category.ID)
		}
		selected = append(selected, provider)
		delete(requested, category.ID)
	}
	if len(requested) > 0 {
		return nil, ErrUnknownCategory
	}
	if len(selected) == 0 {
		return nil, ErrEmptySelection
	}
	return selected, nil
}

func (s *Service) providerMap() map[string]Provider {
	providers := make(map[string]Provider, len(s.providers))
	for _, provider := range s.providers {
		providers[provider.Category().ID] = provider
	}
	return providers
}

func (s *Service) record(ctx context.Context, userID, action, resourceID, result string, metadata map[string]any) {
	if s.audit == nil {
		return
	}
	s.audit.RecordSafe(ctx, audit.EventInput{ActorUserID: userID, ActorType: audit.ActorTypeUser, Action: action, ResourceType: "maintenance_cleanup_plan", ResourceID: resourceID, Result: result, Metadata: metadata})
}

func newConfirmationToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

func cutoffFor(now time.Time, retentionDays int) time.Time {
	return now.Add(-time.Duration(retentionDays) * 24 * time.Hour)
}

func totalCandidates(items []PlanItem) int64 {
	var total int64
	for _, item := range items {
		total += item.CandidateCount
	}
	return total
}

func providerErrorCode(err error, fallback string) string {
	if errors.Is(err, ErrUnsafeOwnership) {
		return "unsafe_ownership"
	}
	if err == nil {
		return fallback
	}
	return fallback
}

func reportMetadata(report Report) map[string]any {
	var deleted int64
	var failed int
	for _, provider := range report.Providers {
		deleted += provider.DeletedCount
		if provider.Status == StatusFailed {
			failed++
		}
	}
	return map[string]any{"deleted_count": deleted, "failed_provider_count": failed}
}
