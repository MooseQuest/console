package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/moosequest/console/internal/core"
)

func sampleEvent() core.Event {
	return core.Event{
		Type:      core.EventComponentDown,
		Title:     "Component api is down",
		Message:   "errors 80/1000 (8.00%)",
		Component: "api",
		At:        time.Unix(1_700_000_000, 0).UTC(),
	}
}

func TestNotify_PostsJSON(t *testing.T) {
	var got struct {
		Type      string `json:"type"`
		Title     string `json:"title"`
		Severity  string `json:"severity"`
		Component string `json:"component"`
	}
	var gotSecret, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotSecret = r.Header.Get("X-Webhook-Secret")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	n := New(srv.URL, WithSecret("s3cr3t"))
	if err := n.Notify(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if got.Type != "component_down" {
		t.Errorf("type = %q", got.Type)
	}
	if got.Title != "Component api is down" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Severity != "critical" {
		t.Errorf("severity = %q, want critical", got.Severity)
	}
	if got.Component != "api" {
		t.Errorf("component = %q", got.Component)
	}
	if gotSecret != "s3cr3t" {
		t.Errorf("X-Webhook-Secret = %q", gotSecret)
	}
}

func TestNotify_NoSecretHeaderWhenUnset(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["X-Webhook-Secret"]
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	if err := New(srv.URL).Notify(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if present {
		t.Error("X-Webhook-Secret header should be absent when no secret is set")
	}
}

func TestNotify_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	if err := New(srv.URL).Notify(context.Background(), sampleEvent()); err == nil {
		t.Fatal("expected error on non-2xx response")
	}
}

func TestNotify_EmptyURL(t *testing.T) {
	if err := New("").Notify(context.Background(), sampleEvent()); err == nil {
		t.Fatal("expected error when no URL is configured")
	}
}
