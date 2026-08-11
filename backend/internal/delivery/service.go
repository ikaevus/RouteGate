package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/audit"
)

var ErrIdempotencyConflict = errors.New("delivery idempotency key already used for a different request")

type deliveryCreator interface {
	Create(context.Context, CreateInput) (Delivery, bool, error)
}

type auditRecorder interface {
	RecordSafe(context.Context, audit.EventInput)
}

type Service struct {
	repository deliveryCreator
	audit      auditRecorder
}

func NewService(repository deliveryCreator, recorder auditRecorder) *Service {
	return &Service{repository: repository, audit: recorder}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Delivery, bool, error) {
	input = normalizeCreateInput(input)
	if err := validateCreateInput(input); err != nil {
		return Delivery{}, false, err
	}
	delivery, created, err := s.repository.Create(ctx, input)
	if err != nil {
		return Delivery{}, false, err
	}
	if !created && !sameCreateRequest(delivery, input) {
		return Delivery{}, false, ErrIdempotencyConflict
	}
	if created {
		s.recordRequested(ctx, delivery)
	}
	return delivery, created, nil
}

func validateCreateInput(input CreateInput) error {
	if input.Channel == "" || len(input.Channel) > 64 {
		return fmt.Errorf("delivery channel is required and must be at most 64 characters")
	}
	if input.Provider == "" || len(input.Provider) > 64 {
		return fmt.Errorf("delivery provider is required and must be at most 64 characters")
	}
	if input.Recipient == "" || len(input.Recipient) > 1024 {
		return fmt.Errorf("delivery recipient is required and must be at most 1024 characters")
	}
	if !validTemplateKey(input.TemplateKey) {
		return fmt.Errorf("delivery template is not supported")
	}
	if !validLocale(input.Locale) {
		return fmt.Errorf("delivery locale is not supported")
	}
	if input.MaxAttempts < 1 || input.MaxAttempts > 20 {
		return fmt.Errorf("delivery max attempts must be between 1 and 20")
	}
	if len(input.IdempotencyKey) > 200 {
		return fmt.Errorf("delivery idempotency key must be at most 200 characters")
	}
	return nil
}

func sameCreateRequest(delivery Delivery, input CreateInput) bool {
	return delivery.VPNAccountID == input.VPNAccountID &&
		delivery.Channel == input.Channel &&
		delivery.Provider == input.Provider &&
		delivery.Recipient == input.Recipient &&
		delivery.TemplateKey == input.TemplateKey &&
		delivery.Locale == input.Locale &&
		delivery.AttachQR == input.AttachQR &&
		delivery.MaxAttempts == input.MaxAttempts &&
		delivery.CreatedByUserID == input.CreatedByUserID
}

func (s *Service) recordRequested(ctx context.Context, delivery Delivery) {
	if s.audit == nil {
		return
	}
	actorType := audit.ActorTypeSystem
	if strings.TrimSpace(delivery.CreatedByUserID) != "" {
		actorType = audit.ActorTypeUser
	}
	s.audit.RecordSafe(ctx, audit.EventInput{
		ActorUserID:  delivery.CreatedByUserID,
		ActorType:    actorType,
		Action:       "delivery.requested",
		ResourceType: "delivery",
		ResourceID:   delivery.ID,
		Result:       audit.ResultSuccess,
		Metadata:     safeAuditMetadata(delivery, "", ""),
	})
}
