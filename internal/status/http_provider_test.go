package status

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/moosequest/console/internal/core"
)

func TestHTTPProviderName(t *testing.T) {
	p := &HTTPProvider{}
	if got := p.Name(); got != "http" {
		t.Fatalf("Name() = %q, want %q", got, "http")
	}
}

func TestHTTPProviderOperational(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := &HTTPProvider{Client: srv.Client()}
	comp := core.Component{Key: "api", Config: map[string]string{"url": srv.URL}}

	check := p.Check(context.Background(), comp)
	if check.State != core.StateOperational {
		t.Fatalf("State = %v, want Operational", check.State)
	}
	if check.Component != "api" {
		t.Fatalf("Component = %q, want %q", check.Component, "api")
	}
	if check.Latency <= 0 {
		t.Fatalf("Latency = %v, want > 0", check.Latency)
	}
	if check.CheckedAt.IsZero() {
		t.Fatal("CheckedAt is zero")
	}
}

func TestHTTPProviderDegradedOn500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := &HTTPProvider{Client: srv.Client()}
	comp := core.Component{Key: "api", Config: map[string]string{"url": srv.URL}}

	check := p.Check(context.Background(), comp)
	if check.State != core.StateDegraded {
		t.Fatalf("State = %v, want Degraded", check.State)
	}
	if check.Latency <= 0 {
		t.Fatalf("Latency = %v, want > 0", check.Latency)
	}
}

func TestHTTPProviderDownOnTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := &HTTPProvider{Client: srv.Client()}
	comp := core.Component{Key: "api", Config: map[string]string{
		"url":     srv.URL,
		"timeout": "20ms",
	}}

	check := p.Check(context.Background(), comp)
	if check.State != core.StateDown {
		t.Fatalf("State = %v, want Down", check.State)
	}
}

func TestHTTPProviderExpectStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // 418
	}))
	defer srv.Close()

	p := &HTTPProvider{Client: srv.Client()}

	// Matching expect_status is operational even though it is not 2xx.
	match := p.Check(context.Background(), core.Component{
		Key:    "api",
		Config: map[string]string{"url": srv.URL, "expect_status": "418"},
	})
	if match.State != core.StateOperational {
		t.Fatalf("matching expect_status: State = %v, want Operational", match.State)
	}

	// Non-matching expect_status is degraded.
	miss := p.Check(context.Background(), core.Component{
		Key:    "api",
		Config: map[string]string{"url": srv.URL, "expect_status": "200"},
	})
	if miss.State != core.StateDegraded {
		t.Fatalf("non-matching expect_status: State = %v, want Degraded", miss.State)
	}
}

func TestHTTPProviderMissingURL(t *testing.T) {
	p := &HTTPProvider{}
	check := p.Check(context.Background(), core.Component{Key: "api"})
	if check.State != core.StateUnknown {
		t.Fatalf("State = %v, want Unknown", check.State)
	}
	if check.Message == "" {
		t.Fatal("expected an explanatory Message")
	}
}
