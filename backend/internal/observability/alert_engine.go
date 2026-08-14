package observability

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AlertEngine struct {
	logger     *slog.Logger
	repository *AlertRepository
	interval   time.Duration
}

func NewAlertEngine(logger *slog.Logger, pool *pgxpool.Pool) *AlertEngine {
	return &AlertEngine{logger: logger, repository: NewAlertRepository(pool), interval: AlertEvaluationInterval}
}

func (e *AlertEngine) Run(ctx context.Context) error {
	e.evaluateSafe(ctx, time.Now().UTC())
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			e.evaluateSafe(ctx, now.UTC())
		}
	}
}

func (e *AlertEngine) evaluateSafe(ctx context.Context, now time.Time) {
	if err := e.EvaluateOnce(ctx, now); err != nil {
		e.logger.Error("observability alert evaluation failed", "error", err)
	}
}

func (e *AlertEngine) EvaluateOnce(ctx context.Context, now time.Time) error {
	checks, err := e.repository.ListCurrentHealth(ctx)
	if err != nil {
		return err
	}
	for _, check := range checks {
		if !check.Required {
			continue
		}
		if err := e.evaluateCheck(ctx, check, now.UTC()); err != nil {
			return err
		}
	}
	return nil
}

func (e *AlertEngine) evaluateCheck(ctx context.Context, check HealthCheck, now time.Time) error {
	fingerprint := alertFingerprint(check.Key, check.Resource)
	active, err := e.repository.ActiveByFingerprint(ctx, fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		active = nil
	} else if err != nil {
		return err
	}

	condition := EvaluateAlertCondition(check, now)
	if !condition.Triggered {
		return e.recoverIfNeeded(ctx, active, now)
	}
	if active == nil {
		_, err := e.repository.CreatePending(ctx, condition, now)
		return err
	}
	if active.RecoveryStartedAt != nil {
		if err := e.repository.ClearRecovery(ctx, active.ID, now); err != nil {
			return err
		}
		active.RecoveryStartedAt = nil
	}

	// Pending tracks the current condition severity in either direction so a
	// critical transient that improves to warning cannot later fire as critical.
	// Once firing, severity is sticky except for warning -> critical escalation.
	severityChanged := active.State == AlertPending && condition.Severity != active.Severity
	if active.State == AlertFiring && severityRank(condition.Severity) > severityRank(active.Severity) {
		severityChanged = true
	}
	if severityChanged {
		if err := e.repository.ChangeSeverity(ctx, *active, condition, now); err != nil {
			return err
		}
		active.Severity = condition.Severity
		active.Summary = condition.Summary
		active.ReasonCode = condition.ReasonCode
	}

	if active.State == AlertPending {
		previousResolvedAt, err := e.repository.LastResolvedAt(ctx, active.Fingerprint)
		if err != nil {
			return err
		}
		fireAfter := effectiveFireDelay(condition.FireAfter, active.StartedAt, previousResolvedAt)
		if !now.Before(active.StartedAt.Add(fireAfter)) {
			return e.repository.FireIfPending(ctx, *active, condition, now)
		}
	}
	if active.State == AlertFiring && severityRank(condition.Severity) < severityRank(active.Severity) {
		condition.Summary = active.Summary
		condition.ReasonCode = active.ReasonCode
	}
	return e.repository.Touch(ctx, active.ID, condition, now)
}

func (e *AlertEngine) recoverIfNeeded(ctx context.Context, active *ActiveAlertRecord, now time.Time) error {
	if active == nil {
		return nil
	}
	// A pending condition that clears before its firing delay is a transient, not
	// an incident. Close it immediately so a later recurrence starts a fresh
	// pending window rather than inheriting time from the transient episode.
	if active.State == AlertPending {
		return e.repository.ResolveIfActive(ctx, *active, now)
	}
	if active.RecoveryStartedAt == nil {
		return e.repository.StartRecovery(ctx, active.ID, now)
	}
	if !now.Before(active.RecoveryStartedAt.Add(AlertRecoveryDelay)) {
		return e.repository.ResolveIfActive(ctx, *active, now)
	}
	return e.repository.StartRecovery(ctx, active.ID, now)
}
