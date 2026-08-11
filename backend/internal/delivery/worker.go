package delivery

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/ikaevus/routegate/backend/internal/audit"
)

type workerRepository interface {
	ClaimNext(context.Context) (*Delivery, error)
	MarkSent(context.Context, string, string) (Delivery, error)
	MarkDelivered(context.Context, string, string) (Delivery, error)
	MarkRetrying(context.Context, string, time.Time, ErrorClass, string) (Delivery, error)
	MarkFailed(context.Context, string, ErrorClass, string) (Delivery, error)
	MarkUncertain(context.Context, string, string) (Delivery, error)
	RecoverSendingAfterRestart(context.Context) ([]Delivery, error)
}

type MaterialResolver interface {
	Resolve(context.Context, Delivery) (TemplateData, error)
}

type messageRenderer interface {
	Render(string, string, TemplateData) (Message, error)
}

type Worker struct {
	repository   workerRepository
	resolver     MaterialResolver
	renderer     messageRenderer
	providers    *Registry
	audit        auditRecorder
	logger       *slog.Logger
	retryPolicy  RetryPolicy
	pollInterval time.Duration
	now          func() time.Time
}

func NewWorker(repository workerRepository, resolver MaterialResolver, renderer messageRenderer, providers *Registry, recorder auditRecorder, logger *slog.Logger) *Worker {
	return &Worker{
		repository:   repository,
		resolver:     resolver,
		renderer:     renderer,
		providers:    providers,
		audit:        recorder,
		logger:       logger,
		retryPolicy:  DefaultRetryPolicy(),
		pollInterval: 2 * time.Second,
		now:          time.Now,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	recovered, err := w.repository.RecoverSendingAfterRestart(ctx)
	if err != nil {
		return err
	}
	for _, delivery := range recovered {
		w.recordLifecycle(ctx, delivery, "delivery.uncertain", audit.ResultFailure)
	}

	for {
		processed, err := w.ProcessNext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if w.logger != nil {
				w.logger.Warn("delivery worker iteration failed", "component", "delivery_storage")
			}
		}
		if processed && err == nil {
			continue
		}

		interval := w.pollInterval
		if interval <= 0 {
			interval = 2 * time.Second
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (w *Worker) ProcessNext(ctx context.Context) (bool, error) {
	delivery, err := w.repository.ClaimNext(ctx)
	if err != nil {
		return false, err
	}
	if delivery == nil {
		return false, nil
	}
	w.recordLifecycle(ctx, *delivery, "delivery.sending", audit.ResultSuccess)

	provider, ok := w.providers.Get(delivery.Provider)
	if !ok {
		return true, w.failBeforeSend(ctx, *delivery, Failure{Class: ErrorClassPermanent, Code: "provider_unavailable"})
	}
	if strings.ToLower(strings.TrimSpace(provider.Channel())) != strings.ToLower(strings.TrimSpace(delivery.Channel)) {
		return true, w.failBeforeSend(ctx, *delivery, Failure{Class: ErrorClassPermanent, Code: "provider_channel_mismatch"})
	}
	if w.resolver == nil {
		return true, w.failBeforeSend(ctx, *delivery, Failure{Class: ErrorClassPermanent, Code: "material_resolver_unavailable"})
	}
	data, err := w.resolver.Resolve(ctx, *delivery)
	if err != nil {
		failure := failureFromError(err, ErrorClassPermanent, "material_resolution_failed")
		return true, w.failBeforeSend(ctx, *delivery, failure)
	}
	if w.renderer == nil {
		return true, w.failBeforeSend(ctx, *delivery, Failure{Class: ErrorClassPermanent, Code: "template_renderer_unavailable"})
	}
	message, err := w.renderer.Render(delivery.TemplateKey, delivery.Locale, data)
	if err != nil {
		failure := failureFromError(err, ErrorClassPermanent, "template_render_failed")
		return true, w.failBeforeSend(ctx, *delivery, failure)
	}
	message.Recipient = delivery.Recipient

	result := provider.Send(ctx, message)
	return true, w.applyProviderResult(ctx, *delivery, result)
}

func (w *Worker) failBeforeSend(ctx context.Context, delivery Delivery, failure Failure) error {
	failure.Class = normalizeErrorClass(failure.Class, ErrorClassPermanent)
	failure.Code = normalizeSafeCode(failure.Code)
	if failure.Class == ErrorClassTransient && delivery.AttemptCount < delivery.MaxAttempts {
		updated, err := w.repository.MarkRetrying(ctx, delivery.ID, w.now().UTC().Add(w.retryPolicy.Delay(delivery.AttemptCount)), failure.Class, failure.Code)
		if err != nil {
			return err
		}
		w.recordLifecycle(ctx, updated, "delivery.retry_scheduled", audit.ResultFailure)
		return nil
	}
	updated, err := w.repository.MarkFailed(ctx, delivery.ID, failure.Class, failure.Code)
	if err != nil {
		return err
	}
	w.recordLifecycle(ctx, updated, "delivery.failed", audit.ResultFailure)
	return nil
}

func (w *Worker) applyProviderResult(ctx context.Context, delivery Delivery, result ProviderResult) error {
	code := normalizeSafeCode(result.ErrorCode)
	switch result.Outcome {
	case OutcomeAccepted:
		updated, err := w.repository.MarkSent(ctx, delivery.ID, strings.TrimSpace(result.ProviderReference))
		if err != nil {
			return err
		}
		w.recordLifecycle(ctx, updated, "delivery.sent", audit.ResultSuccess)
		return nil
	case OutcomeDelivered:
		updated, err := w.repository.MarkDelivered(ctx, delivery.ID, strings.TrimSpace(result.ProviderReference))
		if err != nil {
			return err
		}
		w.recordLifecycle(ctx, updated, "delivery.delivered", audit.ResultSuccess)
		return nil
	case OutcomeRetryableFailure:
		if delivery.AttemptCount < delivery.MaxAttempts {
			updated, err := w.repository.MarkRetrying(ctx, delivery.ID, w.now().UTC().Add(w.retryPolicy.Delay(delivery.AttemptCount)), ErrorClassTransient, code)
			if err != nil {
				return err
			}
			w.recordLifecycle(ctx, updated, "delivery.retry_scheduled", audit.ResultFailure)
			return nil
		}
		updated, err := w.repository.MarkFailed(ctx, delivery.ID, ErrorClassTransient, code)
		if err != nil {
			return err
		}
		w.recordLifecycle(ctx, updated, "delivery.failed", audit.ResultFailure)
		return nil
	case OutcomePermanentFailure:
		updated, err := w.repository.MarkFailed(ctx, delivery.ID, ErrorClassPermanent, code)
		if err != nil {
			return err
		}
		w.recordLifecycle(ctx, updated, "delivery.failed", audit.ResultFailure)
		return nil
	case OutcomeUncertain:
		updated, err := w.repository.MarkUncertain(ctx, delivery.ID, code)
		if err != nil {
			return err
		}
		w.recordLifecycle(ctx, updated, "delivery.uncertain", audit.ResultFailure)
		return nil
	default:
		updated, err := w.repository.MarkUncertain(ctx, delivery.ID, "invalid_provider_outcome")
		if err != nil {
			return err
		}
		w.recordLifecycle(ctx, updated, "delivery.uncertain", audit.ResultFailure)
		return nil
	}
}

func (w *Worker) recordLifecycle(ctx context.Context, delivery Delivery, action, result string) {
	if w.audit == nil {
		return
	}
	w.audit.RecordSafe(ctx, audit.EventInput{
		ActorType:    audit.ActorTypeSystem,
		Action:       action,
		ResourceType: "delivery",
		ResourceID:   delivery.ID,
		Result:       result,
		Metadata:     safeAuditMetadata(delivery, delivery.LastErrorClass, delivery.LastErrorCode),
	})
}

func safeAuditMetadata(delivery Delivery, errorClass ErrorClass, errorCode string) map[string]any {
	metadata := map[string]any{
		"channel":          delivery.Channel,
		"provider":         delivery.Provider,
		"template":         delivery.TemplateKey,
		"locale":           delivery.Locale,
		"status":           string(delivery.Status),
		"attempt":          delivery.AttemptCount,
		"recipient_masked": MaskRecipient(delivery.Recipient),
	}
	if errorClass != "" {
		metadata["error_class"] = string(errorClass)
	}
	if strings.TrimSpace(errorCode) != "" {
		metadata["error_code"] = normalizeSafeCode(errorCode)
	}
	return metadata
}
