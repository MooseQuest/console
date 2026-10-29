package pprofsrv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckAddr(t *testing.T) {
	tests := []struct {
		addr    string
		wantErr bool
	}{
		{"127.0.0.1:6060", false},
		{"[::1]:6060", false},
		{"localhost:6060", false},
		{":6060", true},
		{"0.0.0.0:6060", true},
		{"[::]:6060", true},
		{"10.0.0.5:6060", true},
		{"example.com:6060", true},
		{"6060", true},
	}
	for _, tc := range tests {
		err := CheckAddr(tc.addr)
		if (err != nil) != tc.wantErr {
			t.Errorf("CheckAddr(%q) err=%v, wantErr=%v", tc.addr, err, tc.wantErr)
		}
	}
}

func TestStartGuard(t *testing.T) {
	tests := []struct {
		name        string
		addr        string
		allowRemote bool
		wantErr     string
		wantNil     bool
	}{
		{"disabled", "", false, "", true},
		{"loopback ok", "127.0.0.1:0", false, "", false},
		{"ipv6 loopback ok", "[::1]:0", false, "", false},
		{"localhost ok", "localhost:0", false, "", false},
		{"all interfaces refused", ":0", false, "CONSOLE_PPROF_ALLOW_REMOTE", true},
		{"0.0.0.0 refused", "0.0.0.0:0", false, "non-loopback", true},
		{"private refused", "10.0.0.5:6060", false, "non-loopback", true},
		{"all interfaces allowed with override", ":0", true, "", false},
		{"0.0.0.0 allowed with override", "0.0.0.0:0", true, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Start(tc.addr, tc.allowRemote)
			defer s.Shutdown(context.Background())
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if (s == nil) != tc.wantNil {
				t.Fatalf("server nil = %v, want %v", s == nil, tc.wantNil)
			}
		})
	}
}

func TestFromEnv(t *testing.T) {
	tests := []struct {
		addr, allow string
		wantAddr    string
		wantAllow   bool
	}{
		{"", "", "", false},
		{"127.0.0.1:6060", "", "127.0.0.1:6060", false},
		{":6060", "1", ":6060", true},
		{":6060", "0", ":6060", false},
		{":6060", "true", ":6060", true},
	}
	for _, tc := range tests {
		t.Setenv(EnvAddr, tc.addr)
		t.Setenv(EnvAllowRemote, tc.allow)
		a, r := FromEnv()
		if a != tc.wantAddr || r != tc.wantAllow {
			t.Errorf("FromEnv(%q,%q) = %q,%v", tc.addr, tc.allow, a, r)
		}
	}
}

func TestServesAndShutsDown(t *testing.T) {
	s, err := Start("127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get("http://" + s.Addr() + "/debug/pprof/cmdline")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	c := http.Client{Timeout: time.Second}
	if r, err := c.Get("http://" + s.Addr() + "/debug/pprof/"); err == nil {
		r.Body.Close()
		t.Fatal("listener still accepting after Shutdown")
	}
}

func TestHandlerServesPprof(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/debug/pprof/", nil))
	if rec.Code != 200 {
		t.Fatalf("dedicated mux status %d", rec.Code)
	}
}
