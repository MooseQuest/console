package discord

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

func TestNotify_PostsEmbed(t *testing.T) {
	var payload struct {
		Embeds []struct {
			Title       string
			Description string
			Color       int
			Timestamp   string
			Footer      struct{ Text string }
		} `json:"embeds"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := New(srv.URL).Notify(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(payload.Embeds) != 1 {
		t.Fatalf("expected 1 embed, got %d", len(payload.Embeds))
	}
	e := payload.Embeds[0]
	if e.Title != "Component api is down" {
		t.Errorf("title = %q", e.Title)
	}
	if e.Description != "errors 80/1000 (8.00%)" {
		t.Errorf("description = %q", e.Description)
	}
	if e.Color != 0xDC2626 { // critical
		t.Errorf("color = %d, want critical red", e.Color)
	}
	if e.Timestamp != "2023-11-14T22:13:20Z" {
		t.Errorf("timestamp = %q", e.Timestamp)
	}
	if e.Footer.Text != "Console" {
		t.Errorf("footer = %q", e.Footer.Text)
	}
}

func TestNotify_ColorBySeverity(t *testing.T) {
	cases := map[core.EventType]int{
		core.EventComponentDown:      0xDC2626,
		core.EventComponentDegraded:  0xD97706,
		core.EventComponentRecovered: 0x16A34A,
		core.EventFlagChanged:        0x4F46E5,
	}
	for et, want := range cases {
		if got := colorFor(et.Severity()); got != want {
			t.Errorf("%s -> %#x, want %#x", et, got, want)
		}
	}
}

func TestNotify_OmitsEmptyDescription(t *testing.T) {
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	ev := sampleEvent()
	ev.Message = ""
	if err := New(srv.URL).Notify(context.Background(), ev); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	embed := raw["embeds"].([]any)[0].(map[string]any)
	if _, ok := embed["description"]; ok {
		t.Errorf("description should be omitted when Message is empty")
	}
}

func TestNotify_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid webhook"))
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
