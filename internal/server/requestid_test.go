package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/obs"
)

func TestValidRequestID(t *testing.T) {
	for id, want := range map[string]bool{
		"abc-1.2_X": true, strings.Repeat("a", 64): true,
		"": false, strings.Repeat("a", 65): false, "a b": false, "a\nb": false, "é": false, "a/b": false,
	} {
		if got := validRequestID(id); got != want {
			t.Errorf("validRequestID(%q)=%v want %v", id, got, want)
		}
	}
}

func TestRequestIDAndLogging(t *testing.T) {
	var buf bytes.Buffer
	mem := obs.NewMemory()
	s := New(&app.App{Obs: mem}).WithLogger(NewLogger(&buf, "json"))
	s.mux.HandleFunc("GET /boom/{id}", func(http.ResponseWriter, *http.Request) { panic("kaboom") })
	s.mux.HandleFunc("GET /ctx", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(RequestID(r.Context())))
	})
	h := s.Handler()

	// Valid inbound ID is echoed and visible in context.
	req := httptest.NewRequest("GET", "/ctx?secret=hunter2", nil)
	req.Header.Set(RequestIDHeader, "abc-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get(RequestIDHeader) != "abc-123" || rec.Body.String() != "abc-123" {
		t.Fatalf("id not propagated: hdr=%q body=%q", rec.Header().Get(RequestIDHeader), rec.Body.String())
	}

	// Invalid inbound ID is replaced.
	req = httptest.NewRequest("GET", "/ctx", nil)
	req.Header.Set(RequestIDHeader, "bad id!")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if id := rec.Header().Get(RequestIDHeader); id == "bad id!" || len(id) != 32 {
		t.Fatalf("expected regenerated id, got %q", id)
	}

	// Panic: 500, logged with id and route pattern, not raw path.
	req = httptest.NewRequest("GET", "/boom/42", nil)
	req.Header.Set(RequestIDHeader, "panic-1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 500 {
		t.Fatalf("panic status %d", rec.Code)
	}

	var sawPanic, sawReq bool
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("not json: %q", line)
		}
		for _, k := range []string{"request_id", "method", "route", "status", "duration"} {
			if _, ok := m[k]; !ok {
				t.Errorf("log line missing %s: %s", k, line)
			}
		}
		if m["request_id"] == "panic-1" {
			if m["route"] != "GET /boom/{id}" {
				t.Errorf("route = %v", m["route"])
			}
			if m["msg"] == "panic recovered" {
				sawPanic = true
			} else {
				sawReq = true
			}
		}
	}
	if !sawPanic || !sawReq {
		t.Errorf("missing panic/request lines: %s", buf.String())
	}
	if strings.Contains(buf.String(), "hunter2") || strings.Contains(buf.String(), "/boom/42") {
		t.Errorf("log leaks query or raw path: %s", buf.String())
	}

	// Metrics (outside request-ID) still see the route pattern.
	var m bytes.Buffer
	_ = mem.WritePrometheus(&m)
	if !strings.Contains(m.String(), `route="GET /boom/{id}"`) {
		t.Errorf("metrics lost route pattern:\n%s", m.String())
	}
}

// TestLogsExcludeComponentConfig checks that component config values (which
// may hold secrets) never reach the request log.
func TestLogsExcludeComponentConfig(t *testing.T) {
	var buf bytes.Buffer
	_, a := newTestServer(t)
	hs := httptest.NewServer(New(a).WithLogger(NewLogger(&buf, "text")).Handler())
	defer hs.Close()

	body := `{"key":"db","name":"DB","provider":"http","config":{"url":"http://127.0.0.1:1","token":"s3cr3t-token"}}`
	resp, err := http.Post(hs.URL+"/api/components", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if buf.Len() == 0 {
		t.Fatal("expected request log output")
	}
	if strings.Contains(buf.String(), "s3cr3t-token") {
		t.Errorf("secret leaked into logs: %s", buf.String())
	}
}
