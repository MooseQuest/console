// Package pprofsrv runs the opt-in net/http/pprof listener. Profiling
// endpoints are unauthenticated and can leak memory contents, so the listener
// is off by default, lives on its own mux and port (never DefaultServeMux or
// the main server mux), and refuses non-loopback addresses unless the operator
// explicitly overrides the guard. See docs/security/runtime-hardening.md.
package pprofsrv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"
)

// Environment variables controlling the listener.
const (
	EnvAddr        = "CONSOLE_PPROF_ADDR"
	EnvAllowRemote = "CONSOLE_PPROF_ALLOW_REMOTE"
)

// FromEnv reads the pprof address and remote override from the environment.
// An empty addr means pprof is disabled.
func FromEnv() (addr string, allowRemote bool) {
	addr = strings.TrimSpace(os.Getenv(EnvAddr))
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvAllowRemote))) {
	case "1", "true", "yes", "on":
		allowRemote = true
	}
	return addr, allowRemote
}

// CheckAddr returns an error unless addr is a host:port bound to loopback
// ("localhost" or a loopback IP literal). Hostnames other than "localhost" are
// refused rather than resolved, and an empty host (":6060") means all
// interfaces and is refused.
func CheckAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid pprof address %q: %w", addr, err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing to serve pprof on non-loopback address %q: pprof is unauthenticated; "+
		"use a loopback address such as 127.0.0.1:6060, or set %s=1 to override", addr, EnvAllowRemote)
}

// Handler returns a dedicated mux serving the pprof endpoints under
// /debug/pprof/. It does not touch http.DefaultServeMux.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

// Server is a running pprof listener.
type Server struct {
	srv *http.Server
	ln  net.Listener
}

// Start validates addr (unless allowRemote) and begins serving pprof. It binds
// synchronously so a refused or unbindable address is returned as an error the
// caller can treat as a startup failure. An empty addr returns (nil, nil):
// pprof disabled.
func Start(addr string, allowRemote bool) (*Server, error) {
	if addr == "" {
		return nil, nil
	}
	if !allowRemote {
		if err := CheckAddr(addr); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("pprof listen %q: %w", addr, err)
	}
	s := &Server{
		ln: ln,
		srv: &http.Server{
			Handler:           Handler(),
			ReadHeaderTimeout: 10 * time.Second,
			// No WriteTimeout: CPU profile and trace requests stream for
			// their requested duration.
		},
	}
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "console: pprof server: %v\n", err)
		}
	}()
	return s, nil
}

// Addr returns the bound listener address.
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Shutdown gracefully stops the listener, falling back to a hard close if ctx
// expires (long-running profile requests would otherwise block shutdown).
// It is safe to call on a nil Server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if err := s.srv.Shutdown(ctx); err != nil {
		_ = s.srv.Close()
		return err
	}
	return nil
}
