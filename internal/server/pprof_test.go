package server

import (
	"net/http"
	"testing"

	// Imported so net/http/pprof's init side effect (registering on
	// http.DefaultServeMux) is present in this test binary.
	_ "github.com/moosequest/console/internal/pprofsrv"
)

// TestMainMuxHasNoPprof guards that pprof is never reachable via the main
// server handler; it lives only on the opt-in loopback listener.
func TestMainMuxHasNoPprof(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, p := range []string{"/debug/pprof/", "/debug/pprof/heap", "/debug/pprof/profile", "/debug/pprof/cmdline"} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, resp.StatusCode)
		}
	}
}
