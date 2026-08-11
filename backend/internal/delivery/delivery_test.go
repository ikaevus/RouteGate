package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/audit"
)

type fakeAuditRecorder struct {
	events []audit.EventInput
}

func (f *fakeAuditRecorder) RecordSafe(_ context.Context, input audit.EventInput) {
	f.events = append(f.events, input)
}

type fakeCreator struct {
	delivery Delivery
	created  bool
	input    CreateInput
}

func (f *fakeCreator) Create(_ context.Context, input CreateInput) (Delivery, bool, error) {
	f.input = input
	if f.delivery.ID == "" {
		f.delivery = Delivery{
			ID:              "11111111-1111-1111-1111-111111111111",
			VPNAccountID:    input.VPNAccountID,
			Channel:         input.Channel,
			Provider:        input.Provider,
			Recipient:       input.Recipient,
			TemplateKey:     input.TemplateKey,
			Locale:          input.Locale,
			AttachQR:        input.AttachQR,
			Status:          StatusQueued,
			MaxAttempts:     input.MaxAttempts,
			IdempotencyKey:  input.IdempotencyKey,
			CreatedByUserID: input.CreatedByUserID,
		}
	}
	return f.delivery, f.created, nil
}

type fakeWorkerRepository struct {
	next              *Delivery
	recovered         []Delivery
	marked            Delivery
	retryAt           time.Time
	recoverCalled     bool
	markRetryingCalls int
	markFailedCalls   int
}

func (f *fakeWorkerRepository) ClaimNext(context.Context) (*Delivery, error) {
	next := f.next
	f.next = nil
	return next, nil
}
func (f *fakeWorkerRepository) MarkSent(_ context.Context, id, reference string) (Delivery, error) {
	f.marked = withState(id, StatusSent, "", "")
	f.marked.ProviderReference = reference
	return f.marked, nil
}
func (f *fakeWorkerRepository) MarkDelivered(_ context.Context, id, reference string) (Delivery, error) {
	f.marked = withState(id, StatusDelivered, "", "")
	f.marked.ProviderReference = reference
	return f.marked, nil
}
func (f *fakeWorkerRepository) MarkRetrying(_ context.Context, id string, next time.Time, class ErrorClass, code string) (Delivery, error) {
	f.markRetryingCalls++
	f.retryAt = next
	f.marked = withState(id, StatusRetrying, class, code)
	return f.marked, nil
}
func (f *fakeWorkerRepository) MarkFailed(_ context.Context, id string, class ErrorClass, code string) (Delivery, error) {
	f.markFailedCalls++
	f.marked = withState(id, StatusFailed, class, code)
	return f.marked, nil
}
func (f *fakeWorkerRepository) MarkUncertain(_ context.Context, id, code string) (Delivery, error) {
	f.marked = withState(id, StatusUncertain, ErrorClassUncertain, code)
	return f.marked, nil
}
func (f *fakeWorkerRepository) RecoverSendingAfterRestart(context.Context) ([]Delivery, error) {
	f.recoverCalled = true
	return f.recovered, nil
}

type fakeResolver struct {
	material ResolvedMaterial
	err      error
}

func (f fakeResolver) Resolve(context.Context, Delivery) (ResolvedMaterial, error) {
	return f.material, f.err
}

type fakeProvider struct {
	name     string
	channel  string
	result   ProviderResult
	messages []Message
}

func (f *fakeProvider) Name() string    { return f.name }
func (f *fakeProvider) Channel() string { return f.channel }
func (f *fakeProvider) Send(_ context.Context, message Message) ProviderResult {
	f.messages = append(f.messages, message)
	return f.result
}

func TestRendererUsesCentralizedEnglishAndRussianTemplates(t *testing.T) {
	renderer := NewRenderer()
	data := TemplateData{ProfileName: "Phone", ConnectURL: "https://example.invalid/connect.html#fixture"}
	for _, locale := range []string{"en", "ru"} {
		message, err := renderer.Render(TemplateVPNAccess, locale, data)
		if err != nil {
			t.Fatalf("render %s template: %v", locale, err)
		}
		if !strings.Contains(message.Text, data.ConnectURL) || strings.Contains(strings.ToLower(message.Text), "vless://") {
			t.Fatalf("unsafe or incomplete %s template: %+v", locale, message)
		}
	}
}

func TestServiceIdempotencyAndAuditAreSafe(t *testing.T) {
	creator := &fakeCreator{created: true}
	recorder := &fakeAuditRecorder{}
	service := NewService(creator, recorder)
	input := CreateInput{
		VPNAccountID:    "22222222-2222-2222-2222-222222222222",
		Channel:         "email",
		Provider:        "smtp",
		Recipient:       "felix@example.invalid",
		TemplateKey:     TemplateVPNAccess,
		IdempotencyKey:  "request-1",
		CreatedByUserID: "33333333-3333-3333-3333-333333333333",
	}

	delivery, created, err := service.Create(context.Background(), input)
	if err != nil || !created {
		t.Fatalf("create delivery: created=%v err=%v", created, err)
	}
	if creator.input.Locale != "en" || creator.input.MaxAttempts != 5 {
		t.Fatalf("defaults not applied: %+v", creator.input)
	}
	if len(recorder.events) != 1 || recorder.events[0].Action != "delivery.requested" {
		t.Fatalf("unexpected audit events: %+v", recorder.events)
	}
	masked, _ := recorder.events[0].Metadata["recipient_masked"].(string)
	if masked == delivery.Recipient || strings.Contains(masked, "felix") {
		t.Fatalf("recipient leaked into audit metadata: %q", masked)
	}

	creator.created = false
	creator.delivery = delivery
	recorder.events = nil
	_, created, err = service.Create(context.Background(), input)
	if err != nil || created || len(recorder.events) != 0 {
		t.Fatalf("idempotent replay created=%v err=%v audit=%+v", created, err, recorder.events)
	}

	conflicting := input
	conflicting.Recipient = "other@example.invalid"
	_, _, err = service.Create(context.Background(), conflicting)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting idempotency key error = %v", err)
	}
}

func TestRetryPolicyIsBounded(t *testing.T) {
	policy := RetryPolicy{BaseDelay: time.Second, MaxDelay: 5 * time.Second}
	cases := map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 4: 5 * time.Second, 20: 5 * time.Second}
	for attempt, want := range cases {
		if got := policy.Delay(attempt); got != want {
			t.Fatalf("attempt %d delay=%s want=%s", attempt, got, want)
		}
	}
}

func TestWorkerAcceptedMeansSentNotDelivered(t *testing.T) {
	repository := &fakeWorkerRepository{next: queuedFixture(1, 5)}
	provider := &fakeProvider{name: "test", channel: "email", result: ProviderResult{Outcome: OutcomeAccepted, ProviderReference: "msg-123"}}
	registry, _ := NewRegistry(provider)
	worker := NewWorker(repository, fakeResolver{material: ResolvedMaterial{TemplateData: TemplateData{ConnectURL: "https://example.invalid/connect.html#fixture"}}}, NewRenderer(), registry, nil, nil)

	processed, err := worker.ProcessNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("process: processed=%v err=%v", processed, err)
	}
	if repository.marked.Status != StatusSent {
		t.Fatalf("accepted status=%q want sent", repository.marked.Status)
	}
	if len(provider.messages) != 1 || provider.messages[0].Recipient != "felix@example.invalid" {
		t.Fatalf("unexpected provider messages: %+v", provider.messages)
	}
}

func TestWorkerSchedulesOnlySafeRetryableFailure(t *testing.T) {
	repository := &fakeWorkerRepository{next: queuedFixture(1, 5)}
	provider := &fakeProvider{name: "test", channel: "email", result: ProviderResult{Outcome: OutcomeRetryableFailure, ErrorCode: "temporary_unavailable"}}
	registry, _ := NewRegistry(provider)
	worker := NewWorker(repository, fakeResolver{material: ResolvedMaterial{TemplateData: TemplateData{ConnectURL: "https://example.invalid/connect.html#fixture"}}}, NewRenderer(), registry, nil, nil)
	fixedNow := time.Date(2026, 8, 11, 20, 0, 0, 0, time.UTC)
	worker.now = func() time.Time { return fixedNow }
	worker.retryPolicy = RetryPolicy{BaseDelay: time.Second, MaxDelay: time.Minute}

	_, err := worker.ProcessNext(context.Background())
	if err != nil || repository.marked.Status != StatusRetrying || repository.markRetryingCalls != 1 {
		t.Fatalf("retryable result: err=%v marked=%+v", err, repository.marked)
	}
	if !repository.retryAt.Equal(fixedNow.Add(time.Second)) {
		t.Fatalf("retryAt=%s", repository.retryAt)
	}
}

func TestWorkerUncertainOutcomeNeverAutoRetries(t *testing.T) {
	repository := &fakeWorkerRepository{next: queuedFixture(1, 5)}
	provider := &fakeProvider{name: "test", channel: "email", result: ProviderResult{Outcome: OutcomeUncertain, ErrorCode: "acknowledgement_unknown"}}
	registry, _ := NewRegistry(provider)
	worker := NewWorker(repository, fakeResolver{material: ResolvedMaterial{TemplateData: TemplateData{ConnectURL: "https://example.invalid/connect.html#fixture"}}}, NewRenderer(), registry, nil, nil)

	_, err := worker.ProcessNext(context.Background())
	if err != nil || repository.marked.Status != StatusUncertain || repository.markRetryingCalls != 0 {
		t.Fatalf("uncertain result auto-retried: err=%v marked=%+v", err, repository.marked)
	}
}

func TestWorkerRetryExhaustionBecomesFailed(t *testing.T) {
	repository := &fakeWorkerRepository{next: queuedFixture(5, 5)}
	provider := &fakeProvider{name: "test", channel: "email", result: ProviderResult{Outcome: OutcomeRetryableFailure, ErrorCode: "temporary_unavailable"}}
	registry, _ := NewRegistry(provider)
	worker := NewWorker(repository, fakeResolver{material: ResolvedMaterial{TemplateData: TemplateData{ConnectURL: "https://example.invalid/connect.html#fixture"}}}, NewRenderer(), registry, nil, nil)

	_, err := worker.ProcessNext(context.Background())
	if err != nil || repository.marked.Status != StatusFailed || repository.markRetryingCalls != 0 || repository.markFailedCalls != 1 {
		t.Fatalf("retry exhaustion: err=%v marked=%+v", err, repository.marked)
	}
}

func TestWorkerRecoveryAuditsRestartedSendingAsUncertain(t *testing.T) {
	recovered := withState("44444444-4444-4444-4444-444444444444", StatusUncertain, ErrorClassUncertain, "manager_restart")
	repository := &fakeWorkerRepository{recovered: []Delivery{recovered}}
	recorder := &fakeAuditRecorder{}
	registry, _ := NewRegistry()
	worker := NewWorker(repository, nil, NewRenderer(), registry, recorder, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := worker.Run(ctx); err != nil || !repository.recoverCalled {
		t.Fatalf("recovery err=%v called=%v", err, repository.recoverCalled)
	}
	if len(recorder.events) != 1 || recorder.events[0].Action != "delivery.uncertain" {
		t.Fatalf("restart audit=%+v", recorder.events)
	}
}

func TestSafetyHelpersDoNotExposeRecipientOrRawError(t *testing.T) {
	if got := MaskRecipient("felix@example.invalid"); got != "f***@example.invalid" {
		t.Fatalf("masked email=%q", got)
	}
	if got := normalizeSafeCode("timeout: credential-value"); got != "provider_error" {
		t.Fatalf("unsafe provider code=%q", got)
	}
}

func withState(id string, status Status, class ErrorClass, code string) Delivery {
	return Delivery{ID: id, Channel: "email", Provider: "test", Recipient: "f@example.invalid", TemplateKey: TemplateVPNAccess, Locale: "en", Status: status, AttemptCount: 1, MaxAttempts: 5, LastErrorClass: class, LastErrorCode: code}
}

func queuedFixture(attempt, maxAttempts int) *Delivery {
	return &Delivery{ID: "55555555-5555-5555-5555-555555555555", Channel: "email", Provider: "test", Recipient: "felix@example.invalid", TemplateKey: TemplateVPNAccess, Locale: "en", Status: StatusSending, AttemptCount: attempt, MaxAttempts: maxAttempts}
}
