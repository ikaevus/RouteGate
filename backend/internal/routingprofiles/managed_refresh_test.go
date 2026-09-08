package routingprofiles

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseManagedRuleSetSnapshot(t *testing.T) {
	snapshot, err := parseManagedRuleSetSnapshot([]byte(`{"version":1,"rules":[{"domain_suffix":["example.org"],"ip_cidr":["203.0.113.0/24"]}]}`))
	if err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	var parsed ManagedRuleSetSnapshot
	if err := json.Unmarshal(snapshot, &parsed); err != nil {
		t.Fatalf("decode canonical snapshot: %v", err)
	}
	if len(parsed.Rules) != 1 || parsed.Rules[0].DomainSuffixes[0] != "example.org" {
		t.Fatalf("unexpected snapshot: %+v", parsed)
	}
	if _, err := parseManagedRuleSetSnapshot([]byte(`{"version":1,"rules":[]}`)); err == nil {
		t.Fatal("expected empty snapshot rejection")
	}
}

type fakeManagedRefreshRepository struct {
	record    managedRuleSetRecord
	failed    bool
	succeeded bool
}

func (f *fakeManagedRefreshRepository) GetManagedRuleSet(context.Context, string) (managedRuleSetRecord, error) {
	return f.record, nil
}
func (f *fakeManagedRefreshRepository) MarkManagedRuleSetRefreshFailed(_ context.Context, _ string, message string) (ManagedRuleSet, error) {
	f.failed = message != ""
	return f.record.ManagedRuleSet, nil
}
func (f *fakeManagedRefreshRepository) MarkManagedRuleSetRefreshSucceeded(_ context.Context, _ string, snapshot json.RawMessage) (ManagedRuleSet, error) {
	f.succeeded = true
	f.record.Snapshot = snapshot
	return f.record.ManagedRuleSet, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestRefreshFailurePreservesLastKnownGoodSnapshot(t *testing.T) {
	lastGood := json.RawMessage(`{"version":1,"rules":[{"domain":["working.example"]}]}`)
	repository := &fakeManagedRefreshRepository{record: managedRuleSetRecord{ManagedRuleSet: ManagedRuleSet{ID: "set-1", SourceURL: "https://8.8.8.8/rules.json"}, Snapshot: append(json.RawMessage(nil), lastGood...)}}
	refresher := NewManagedRuleSetRefresher(repository)
	refresher.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"version":1,"rules":[]}`)), Header: make(http.Header)}, nil
	})}
	if _, err := refresher.Refresh(context.Background(), "set-1"); err == nil {
		t.Fatal("expected refresh error")
	}
	if !repository.failed || repository.succeeded {
		t.Fatalf("unexpected persistence calls: failed=%v succeeded=%v", repository.failed, repository.succeeded)
	}
	if !jsonEqual(repository.record.Snapshot, lastGood) {
		t.Fatalf("last-known-good snapshot changed: %s", repository.record.Snapshot)
	}
}

func jsonEqual(left, right []byte) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && !errors.Is(compareJSON(a, b), errJSONDifferent)
}

var errJSONDifferent = errors.New("different JSON")

func compareJSON(a, b any) error {
	if string(mustJSON(a)) != string(mustJSON(b)) {
		return errJSONDifferent
	}
	return nil
}
func mustJSON(value any) []byte { result, _ := json.Marshal(value); return result }
