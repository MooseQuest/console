package slack

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

func TestNotify_PostsAttachment(t *testing.T) {
	var payload struct {
		Attachments []struct {
			Color, Title, Text, Footer string
			Ts                         int64
		} `json:"attachments"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	if err := New(srv.URL).Notify(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(payload.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(payload.Attachments))
	}
	a := payload.Attachments[0]
	if a.Title != "Component api is down" {
		t.Errorf("title = %q", a.Title)
	}
	if a.Text != "errors 80/1000 (8.00%)" {
		t.Errorf("text = %q", a.Text)
	}
	if a.Color != "#dc2626" { // critical
		t.Errorf("color = %q, want critical red", a.Color)
	}
	if a.Ts != 1_700_000_000 {
		t.Errorf("ts = %d", a.Ts)
	}
}

func TestNotify_ColorBySeverity(t *testing.T) {
	cases := map[core.EventType]string{
		core.EventComponentDown:      "#dc2626",
		core.EventComponentDegraded:  "#d97706",
		core.EventComponentRecovered: "#16a34a",
		core.EventFlagChanged:        "#4f46e5",
	}
	for et, want := range cases {
		if got := colorFor(et.Severity()); got != want {
			t.Errorf("%s -> %s, want %s", et, got, want)
		}
	}
}

func TestNotify_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid_payload"))
	}))
	defer srv.Close()
	if err := New(srv.URL).Notify(context.Background(), sampleEvent()); err == nil {
		t.Fatal("expected error on non-2xx response")
	}
}

func TestNotify_EmptyURL(t *testing.T) {
	if err := New("").Notify(context.Background(), sampleEvent()); err == nil {
		t.Fatal("expected error when no webhook URL is configured")
	}
}
