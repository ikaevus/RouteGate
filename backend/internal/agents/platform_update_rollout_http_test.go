package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const rolloutHTTPTestID = "550e8400-e29b-41d4-a716-446655440000"

type rolloutHTTPFakeRepository struct {
	*fakeAgentAPIRepository
	persistCalls int
	advanceCalls int
	plan         PlatformUpdateRolloutPlan
	view         PlatformUpdateRolloutView
	result       PlatformUpdateRolloutStepResult
}

func newRolloutHTTPFakeRepository() *rolloutHTTPFakeRepository {
	return &rolloutHTTPFakeRepository{fakeAgentAPIRepository: &fakeAgentAPIRepository{}, view: PlatformUpdateRolloutView{ID: rolloutHTTPTestID}}
}
func (f *rolloutHTTPFakeRepository) PersistPlatformUpdateRolloutPlanIdempotent(_ context.Context, plan PlatformUpdateRolloutPlan, _, _ string) (string, bool, error) {
	f.persistCalls++
	f.plan = plan
	return rolloutHTTPTestID, false, nil
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
