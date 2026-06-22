package pagerduty

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/moosequest/console/internal/core"
)

func downEvent() core.Event {
	return core.Event{
		Type:      core.EventComponentDown,
		Title:     "Component api is down",
		Message:   "errors 80/1000 (8.00%)",
		Component: "api",
		At:        time.Unix(1_700_000_000, 0).UTC(),
	}
}

// capture runs a notifier against a test server and returns the decoded request
// body, or nil if no request was made.
func capture(t *testing.T, ev core.Event) map[string]any {
	t.Helper()
	var got map[string]any
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"success","dedup_key":"console/api"}`))
	}))
	defer srv.Close()

	if err := New("rk-123", WithEndpoint(srv.URL)).Notify(context.Background(), ev); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if hits == 0 {
		return nil
	}
	return got
}

func TestNotify_TriggerOnDown(t *testing.T) {
	got := capture(t, downEvent())
	if got["event_action"] != "trigger" {
		t.Errorf("event_action = %v, want trigger", got["event_action"])
	}
	if got["routing_key"] != "rk-123" {
		t.Errorf("routing_key = %v", got["routing_key"])
	}
	if got["dedup_key"] != "console/api" {
		t.Errorf("dedup_key = %v", got["dedup_key"])
	}
	p, ok := got["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing on trigger")
	}
	if p["severity"] != "critical" {
		t.Errorf("severity = %v, want critical", p["severity"])
	}
	if p["summary"] != "Component api is down" {
		t.Errorf("summary = %v", p["summary"])
	}
	if p["source"] != "console" {
		t.Errorf("source = %v", p["source"])
	}
}

func TestNotify_DegradedIsWarning(t *testing.T) {
	ev := downEvent()
	ev.Type = core.EventComponentDegraded
	got := capture(t, ev)
	p := got["payload"].(map[string]any)
	if p["severity"] != "warning" {
		t.Errorf("severity = %v, want warning", p["severity"])
	}
}

func TestNotify_ResolveOnRecovered(t *testing.T) {
	ev := downEvent()
	ev.Type = core.EventComponentRecovered
	got := capture(t, ev)
	if got["event_action"] != "resolve" {
		t.Errorf("event_action = %v, want resolve", got["event_action"])
	}
	if got["dedup_key"] != "console/api" {
		t.Errorf("dedup_key = %v, want match for correlation", got["dedup_key"])
	}
	if _, ok := got["payload"]; ok {
		t.Errorf("resolve must not carry a payload")
	}
}

func TestNotify_SkipsFlagChange(t *testing.T) {
	ev := core.Event{Type: core.EventFlagChanged, Title: "flag x updated", Flag: "x", At: time.Unix(1_700_000_000, 0).UTC()}
	if got := capture(t, ev); got != nil {
		t.Errorf("flag change should not page PagerDuty, but a request was made: %v", got)
	}
}

func TestNotify_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"invalid event","errors":["routing_key invalid"]}`))
	}))
	defer srv.Close()
	if err := New("rk-123", WithEndpoint(srv.URL)).Notify(context.Background(), downEvent()); err == nil {
		t.Fatal("expected error on non-2xx response")
	}
}

func TestNotify_EmptyRoutingKey(t *testing.T) {
	if err := New("").Notify(context.Background(), downEvent()); err == nil {
		t.Fatal("expected error when no routing key is configured")
	}
}
