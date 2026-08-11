package delivery

import (
	"context"
	"time"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusSending   Status = "sending"
	StatusRetrying  Status = "retrying"
	StatusSent      Status = "sent"
	StatusDelivered Status = "delivered"
	StatusFailed    Status = "failed"
	StatusUncertain Status = "uncertain"
)

type Outcome string

const (
	OutcomeAccepted         Outcome = "accepted"
	OutcomeDelivered        Outcome = "delivered"
	OutcomeRetryableFailure Outcome = "retryable_failure"
	OutcomePermanentFailure Outcome = "permanent_failure"
	OutcomeUncertain        Outcome = "uncertain"
)

type ErrorClass string

const (
	ErrorClassTransient ErrorClass = "transient"
	ErrorClassPermanent ErrorClass = "permanent"
	ErrorClassUncertain ErrorClass = "uncertain"
)

const (
	TemplateVPNAccess          = "vpn_access"
	TemplateVPNAccessReissued  = "vpn_access_reissued"
	TemplateSystemNotification = "system_notification"
)

type Delivery struct {
	ID                string
	VPNAccountID      string
	Channel           string
	Provider          string
	Recipient         string
	TemplateKey       string
	Locale            string
	AttachQR          bool
	Status            Status
	AttemptCount      int
	MaxAttempts       int
	NextAttemptAt     *time.Time
	AttemptStartedAt  *time.Time
	ProviderReference string
	LastErrorClass    ErrorClass
	LastErrorCode     string
	IdempotencyKey    string
	CreatedByUserID   string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	SentAt            *time.Time
	CompletedAt       *time.Time
}

type CreateInput struct {
	VPNAccountID    string
	Channel         string
	Provider        string
	Recipient       string
	TemplateKey     string
	Locale          string
	AttachQR        bool
	MaxAttempts     int
	IdempotencyKey  string
	CreatedByUserID string
}

type Attachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

type Message struct {
	Recipient   string
	Subject     string
	Text        string
	HTML        string
	Attachments []Attachment
}

type ProviderResult struct {
	Outcome           Outcome
	ProviderReference string
	ErrorClass        ErrorClass
	ErrorCode         string
}

type Provider interface {
	Name() string
	Channel() string
	Send(context.Context, Message) ProviderResult
}

type TemplateData struct {
	RecipientName string
	ProfileName   string
	ConnectURL    string
	Title         string
	Message       string
}

type ResolvedMaterial struct {
	TemplateData TemplateData
	Attachments  []Attachment
}

type Failure struct {
	Class ErrorClass
	Code  string
}

func (f Failure) Error() string {
	return f.Code
}
