package observability

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const notificationPollInterval = 2 * time.Second

type SystemNotificationCreator interface {
	CreateSystemNotification(context.Context, string, string, string, string, string) (string, error)
}

type NotificationWorker struct {
	repository *NotificationRepository
	creator    SystemNotificationCreator
	logger     *slog.Logger
}

func NewNotificationWorker(repository *NotificationRepository, creator SystemNotificationCreator, logger *slog.Logger) *NotificationWorker {
	return &NotificationWorker{repository: repository, creator: creator, logger: logger}
}

func (w *NotificationWorker) Run(ctx context.Context) error {
	for {
		processed, err := w.ProcessNext(ctx)
		if err != nil && w.logger != nil {
			w.logger.Warn("notification expansion failed", "error", err)
		}
		if processed && err == nil {
			continue
		}
		timer := time.NewTimer(notificationPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (w *NotificationWorker) ProcessNext(ctx context.Context) (bool, error) {
	intent, err := w.repository.ClaimNext(ctx)
	if err != nil || intent == nil {
		return false, err
	}
	if w.creator == nil {
		return true, fmt.Errorf("system notification creator is unavailable")
	}
	recipients, err := w.repository.ListEnabledRecipients(ctx)
	if err != nil {
		return true, err
	}
	for _, recipient := range recipients {
		deliveryID, err := w.creator.CreateSystemNotification(
			ctx,
			recipient.Channel,
			recipient.Provider,
			recipient.Address,
			recipient.Locale,
			notificationDeliveryIdempotencyKey(intent.ID, recipient.ID),
		)
		if err != nil {
			return true, err
		}
		if err := w.repository.LinkDelivery(ctx, intent.ID, recipient.ID, deliveryID); err != nil {
			return true, err
		}
	}
	return true, w.repository.MarkExpanded(ctx, intent.ID)
}

func notificationDeliveryIdempotencyKey(intentID, recipientID string) string {
	return fmt.Sprintf("alert-notification:%s:%s", strings.TrimSpace(intentID), strings.TrimSpace(recipientID))
}
