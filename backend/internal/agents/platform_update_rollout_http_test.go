package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
)

const rolloutHTTPTestID = "550e8400-e29b-41d4-a716-446655440000"
const rolloutHTTPTestUserID = "550e8400-e29b-41d4-a716-446655440099"

type rolloutHTTPFakeRepository struct {
	*fakeAgentAPIRepository
	persistCalls int
	advanceCalls int
	plan         PlatformUpdateRolloutPlan
	view         PlatformUpdateRolloutView
	result       PlatformUpdateRolloutStepResult
	persistErr   error
}

func newRolloutHTTPFakeRepository() *rolloutHTTPFakeRepository {
	return &rolloutHTTPFakeRepository{fakeAgentAPIRepository: &fakeAgentAPIRepository{}, view: PlatformUpdateRolloutView{ID: rolloutHTTPTestID}}
}
func (f *rolloutHTTPFakeRepository) PersistPlatformUpdateRolloutPlanIdempotent(_ context.Context, plan PlatformUpdateRolloutPlan, _, _ string) (string, bool, error) {
	f.persistCalls++
	f.plan = plan
	return rolloutHTTPTestID, false, f.persistErr
}
func (f *rolloutHTTPFakeRepository) GetPlatformUpdateRollout(context.Context, string) (PlatformUpdateRolloutView, error) {
	return f.view, nil
}
func (f *rolloutHTTPFakeRepository) AdvancePlatformUpdateRollout(context.Context, string) (PlatformUpdateRolloutStepResult, error) {
	f.advanceCalls++
	return f.result, nil
}

func rolloutCreateRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/platform-update-rollouts", strings.NewReader(body))
	r.Header.Set("Idempotency-Key", "550e8400-e29b-41d4-a716-446655440000")
	return r
}

func TestCreatePlatformUpdateRolloutPreservesOrderedCanonicalMembership(t *testing.T) {
	f := newRolloutHTTPFakeRepository()
	h := testAgentHandler(f)
	w := httptest.NewRecorder()
	h.CreatePlatformUpdateRollout(w, rolloutCreateRequest(`{"targetVersion":"v1.2.3","serverIds":["550e8400-e29b-41d4-a716-446655440002","550e8400-e29b-41d4-a716-446655440001"]}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if f.persistCalls != 1 || f.plan.Entries[0].ServerID != "550e8400-e29b-41d4-a716-446655440002" {
		t.Fatalf("ordered plan not persisted: %+v", f.plan)
	}
}

func TestCreatePlatformUpdateRolloutRejectsUnknownSelectorBeforeWork(t *testing.T) {
	f := newRolloutHTTPFakeRepository()
	h := testAgentHandler(f)
	w := httptest.NewRecorder()
	h.CreatePlatformUpdateRollout(w, rolloutCreateRequest(`{"targetVersion":"v1.2.3","serverIds":["550e8400-e29b-41d4-a716-446655440001"],"jobId":"550e8400-e29b-41d4-a716-446655440000"}`))
	if w.Code != http.StatusBadRequest || f.persistCalls != 0 {
		t.Fatalf("status=%d calls=%d", w.Code, f.persistCalls)
	}
}

func TestCreatePlatformUpdateRolloutEnforcesPreWorkBounds(t *testing.T) {
	for name, body := range map[string]string{
		"bytes":   `{"targetVersion":"v1.2.3","serverIds":[]} ` + strings.Repeat(" ", maxPlatformUpdateRolloutCreateRequestBytes),
		"members": `{"targetVersion":"v1.2.3","serverIds":["` + strings.Repeat("550e8400-e29b-41d4-a716-446655440001\",\"", maxPlatformUpdateRolloutMembers) + `550e8400-e29b-41d4-a716-446655440002"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newRolloutHTTPFakeRepository()
			w := httptest.NewRecorder()
			testAgentHandler(f).CreatePlatformUpdateRollout(w, rolloutCreateRequest(body))
			if w.Code != http.StatusBadRequest || f.persistCalls != 0 {
				t.Fatalf("status=%d calls=%d", w.Code, f.persistCalls)
			}
		})
	}
}

func TestAdvancePlatformUpdateRolloutRequiresEmptyBodyAndCallsOnce(t *testing.T) {
	f := newRolloutHTTPFakeRepository()
	h := testAgentHandler(f)
	bad := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	bad.SetPathValue("rollout_id", rolloutHTTPTestID)
	w := httptest.NewRecorder()
	h.AdvancePlatformUpdateRollout(w, bad)
	if w.Code != http.StatusBadRequest || f.advanceCalls != 0 {
		t.Fatalf("nonempty status=%d calls=%d", w.Code, f.advanceCalls)
	}
	good := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	good.SetPathValue("rollout_id", rolloutHTTPTestID)
	w = httptest.NewRecorder()
	h.AdvancePlatformUpdateRollout(w, good)
	if w.Code != http.StatusOK || f.advanceCalls != 1 {
		t.Fatalf("empty status=%d calls=%d", w.Code, f.advanceCalls)
	}
}

func TestAdvancePlatformUpdateRolloutUsesBoundedHTTPDTO(t *testing.T) {
	f := newRolloutHTTPFakeRepository()
	f.result = PlatformUpdateRolloutStepResult{
		RolloutID: rolloutHTTPTestID, RolloutStatus: PlatformUpdateRolloutFailed,
		ServerID: rolloutHTTPTestID, Action: PlatformUpdateRolloutStepFailed,
		ErrorCode: "node_update_failed", BlockerCode: "node_update_failed",
	}
	r := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	r.SetPathValue("rollout_id", rolloutHTTPTestID)
	w := httptest.NewRecorder()
	testAgentHandler(f).AdvancePlatformUpdateRollout(w, r)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"rolloutId", "rolloutStatus", "serverId", "action", "errorCode", "blockerCode"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("response missing %q: %s", key, w.Body.String())
		}
	}
	if _, ok := body["RolloutID"]; ok {
		t.Fatalf("internal result field leaked: %s", w.Body.String())
	}
}

type rolloutAuditRepository struct{ inputs []audit.EventInput }

func (r *rolloutAuditRepository) Create(_ context.Context, input audit.EventInput) (audit.Event, error) {
	r.inputs = append(r.inputs, input)
	return audit.Event{}, nil
}

func TestCreatePlatformUpdateRolloutAuditsIdempotencyConflictWithoutKey(t *testing.T) {
	f := newRolloutHTTPFakeRepository()
	f.persistErr = ErrPlatformUpdateRolloutIdempotencyConflict
	audits := &rolloutAuditRepository{}
	h := testAgentHandler(f)
	h.audit = audit.NewRecorderWithRepository(nil, audits)
	r := rolloutCreateRequest(`{"targetVersion":"v1.2.3","serverIds":["550e8400-e29b-41d4-a716-446655440001"]}`)
	r = r.WithContext(auth.ContextWithUser(r.Context(), auth.AuthenticatedUser{UserProfile: auth.UserProfile{ID: rolloutHTTPTestUserID}}))
	w := httptest.NewRecorder()
	h.CreatePlatformUpdateRollout(w, r)
	if w.Code != http.StatusConflict || len(audits.inputs) != 1 {
		t.Fatalf("status=%d audits=%d", w.Code, len(audits.inputs))
	}
	event := audits.inputs[0]
	if event.Result != audit.ResultFailure || event.Metadata["reason"] != "idempotency_conflict" {
		t.Fatalf("unexpected audit: %+v", event)
	}
	if event.ActorUserID != rolloutHTTPTestUserID {
		t.Fatalf("audit actor=%q, want %q", event.ActorUserID, rolloutHTTPTestUserID)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), r.Header.Get("Idempotency-Key")) {
		t.Fatalf("audit leaked idempotency key: %s", encoded)
	}
}

func TestCreatePlatformUpdateRolloutAuditsPostDecodeRejection(t *testing.T) {
	f := newRolloutHTTPFakeRepository()
	audits := &rolloutAuditRepository{}
	h := testAgentHandler(f)
	h.audit = audit.NewRecorderWithRepository(nil, audits)
	w := httptest.NewRecorder()
	h.CreatePlatformUpdateRollout(w, rolloutCreateRequest(`{"targetVersion":"v1.2.3","serverIds":[]} trailing`))
	if w.Code != http.StatusBadRequest || len(audits.inputs) != 1 {
		t.Fatalf("status=%d audits=%d", w.Code, len(audits.inputs))
	}
	if audits.inputs[0].Metadata["reason"] != "invalid_request" {
		t.Fatalf("unexpected rejection audit: %+v", audits.inputs)
	}
}

func TestAdvancePlatformUpdateRolloutAuditsBoundedRejections(t *testing.T) {
	f := newRolloutHTTPFakeRepository()
	audits := &rolloutAuditRepository{}
	h := testAgentHandler(f)
	h.audit = audit.NewRecorderWithRepository(nil, audits)

	invalidBody := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	invalidBody.SetPathValue("rollout_id", rolloutHTTPTestID)
	w := httptest.NewRecorder()
	h.AdvancePlatformUpdateRollout(w, invalidBody)
	if w.Code != http.StatusBadRequest || f.advanceCalls != 0 {
		t.Fatalf("invalid body status=%d calls=%d", w.Code, f.advanceCalls)
	}

	invalidID := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	invalidID.SetPathValue("rollout_id", "not-a-rollout")
	w = httptest.NewRecorder()
	h.AdvancePlatformUpdateRollout(w, invalidID)
	if w.Code != http.StatusNotFound || f.advanceCalls != 0 {
		t.Fatalf("invalid id status=%d calls=%d", w.Code, f.advanceCalls)
	}

	if len(audits.inputs) != 2 {
		t.Fatalf("rejection audits=%d, want 2", len(audits.inputs))
	}
	if audits.inputs[0].Metadata["reason"] != "invalid_request" || audits.inputs[0].ResourceID != rolloutHTTPTestID {
		t.Fatalf("invalid-body audit=%+v", audits.inputs[0])
	}
	if audits.inputs[1].Metadata["reason"] != "rollout_not_found" || audits.inputs[1].ResourceID != "" {
		t.Fatalf("invalid-id audit=%+v", audits.inputs[1])
	}
}

func TestGetPlatformUpdateRolloutExposesBoundedDurableReasonCodes(t *testing.T) {
	f := newRolloutHTTPFakeRepository()
	f.view = PlatformUpdateRolloutView{
		ID: rolloutHTTPTestID, Status: PlatformUpdateRolloutFailed, ErrorCode: "node_update_failed",
		Entries: []PlatformUpdateRolloutEntryView{{ServerID: rolloutHTTPTestID, BlockerCode: "node_update_failed"}},
	}
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.SetPathValue("rollout_id", rolloutHTTPTestID)
	w := httptest.NewRecorder()
	testAgentHandler(f).GetPlatformUpdateRollout(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"errorCode":"node_update_failed"`) || !strings.Contains(w.Body.String(), `"blockerCode":"node_update_failed"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
