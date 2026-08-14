package delivery

import "context"

type SystemNotificationCreator struct {
	service *Service
}

func NewSystemNotificationCreator(service *Service) *SystemNotificationCreator {
	return &SystemNotificationCreator{service: service}
}

func (c *SystemNotificationCreator) CreateSystemNotification(ctx context.Context, channel, provider, recipient, locale, idempotencyKey string) (string, error) {
	item, _, err := c.service.Create(ctx, CreateInput{
		Channel: channel,
		Provider: provider,
		Recipient: recipient,
		TemplateKey: TemplateSystemNotification,
		Locale: locale,
		MaxAttempts: 5,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return "", err
	}
	return item.ID, nil
}
