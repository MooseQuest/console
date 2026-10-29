package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/moosequest/console/internal/core"
)

// httpBackend is the -addr Backend: it drives a running `console serve` over its
// JSON API. It lets the MCP server target a shared or remote Console instead of
// opening the local store. When the server enforces auth, supply a bearer token
// with WithToken; it is sent on every request and never logged or echoed.
type httpBackend struct {
	base  string
	http  *http.Client
	token string
}

// HTTPOption configures NewHTTPBackend.
type HTTPOption func(*httpOptions)

type httpOptions struct {
	token         string
	insecureToken bool
}

// WithToken makes the backend send "Authorization: Bearer <token>" on every
// request. An empty token sends no header.
func WithToken(token string) HTTPOption {
	return func(o *httpOptions) { o.token = token }
}

// WithInsecureToken allows sending the token over plain http:// to a
// non-loopback host. Off by default.
func WithInsecureToken(allow bool) HTTPOption {
	return func(o *httpOptions) { o.insecureToken = allow }
}

// ResolveToken picks the bearer token from a token file or the environment
// value (CONSOLE_TOKEN). If both are set the file wins. Surrounding whitespace
// (including the trailing newline) is trimmed. Errors never contain the token.
func ResolveToken(env, file string) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("mcp: read -token-file: %w", err)
		}
		tok := strings.TrimSpace(string(b))
		if tok == "" {
			return "", fmt.Errorf("mcp: -token-file %q is empty", file)
		}
		return tok, nil
	}
	return strings.TrimSpace(env), nil
}

// isLoopbackHost reports whether host (no port) is localhost or a loopback IP.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// NewHTTPBackend builds a Backend that talks to the Console JSON API at addr.
// addr may be a bare host:port ("127.0.0.1:8080") or a full URL; a missing
// scheme defaults to http.
func NewHTTPBackend(addr string, opts ...HTTPOption) (Backend, error) {
	var o httpOptions
	for _, fn := range opts {
		fn(&o)
	}
	if addr == "" {
		return nil, fmt.Errorf("mcp: empty -addr")
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("mcp: invalid -addr %q: %w", addr, err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("mcp: invalid -addr %q: no host", addr)
	}
	if o.token != "" && u.Scheme == "http" && !o.insecureToken && !isLoopbackHost(u.Hostname()) {
		return nil, fmt.Errorf("mcp: refusing to send a bearer token over plain http to non-loopback host %q; use https:// or pass -insecure-token", u.Hostname())
	}
	base := strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/")
	return &httpBackend{base: base, token: o.token, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

// do issues a JSON request and decodes a JSON response into out (when non-nil).
// body, when non-nil, is marshalled as the request body.
func (b *httpBackend) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if b.token != "" {
		req.Header.Set("Authorization", "Bearer "+b.token)
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return b.authError(method, path, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, bytes.TrimSpace(snippet))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// authError builds an actionable error for a 401/403 naming the scope the call
// needs. It never includes the token.
func (b *httpBackend) authError(method, path string, status int) error {
	scope := "write"
	switch {
	case method == http.MethodGet:
		scope = "read"
	case strings.HasSuffix(path, "/evaluate"):
		scope = "eval"
	}
	if b.token == "" {
		return fmt.Errorf("%s %s: status %d: the server requires authentication; set CONSOLE_TOKEN or -token-file to a token with the %q scope", method, path, status, scope)
	}
	return fmt.Errorf("%s %s: status %d: the server rejected the configured token; it needs the %q scope (read tools need a read token, -write tools need a write token)", method, path, status, scope)
}

func (b *httpBackend) ListFlags(ctx context.Context) ([]core.Flag, error) {
	var fs []core.Flag
	return fs, b.do(ctx, http.MethodGet, "/api/flags", nil, &fs)
}

func (b *httpBackend) GetFlag(ctx context.Context, key string) (core.Flag, error) {
	var f core.Flag
	return f, b.do(ctx, http.MethodGet, "/api/flags/"+url.PathEscape(key), nil, &f)
}

func (b *httpBackend) CreateFlag(ctx context.Context, f core.Flag) error {
	return b.do(ctx, http.MethodPost, "/api/flags", f, nil)
}

func (b *httpBackend) UpdateFlag(ctx context.Context, f core.Flag) error {
	return b.do(ctx, http.MethodPut, "/api/flags/"+url.PathEscape(f.Key), f, nil)
}

func (b *httpBackend) DeleteFlag(ctx context.Context, key string) error {
	return b.do(ctx, http.MethodDelete, "/api/flags/"+url.PathEscape(key), nil, nil)
}

func (b *httpBackend) EvaluateFlag(ctx context.Context, key string, subj core.Subject) (core.Evaluation, error) {
	var ev core.Evaluation
	return ev, b.do(ctx, http.MethodPost, "/api/flags/"+url.PathEscape(key)+"/evaluate", subj, &ev)
}

func (b *httpBackend) ListComponents(ctx context.Context) ([]core.Component, error) {
	var cs []core.Component
	return cs, b.do(ctx, http.MethodGet, "/api/components", nil, &cs)
}

func (b *httpBackend) GetComponent(ctx context.Context, key string) (core.Component, error) {
	var c core.Component
	return c, b.do(ctx, http.MethodGet, "/api/components/"+url.PathEscape(key), nil, &c)
}

func (b *httpBackend) CreateComponent(ctx context.Context, c core.Component) error {
	return b.do(ctx, http.MethodPost, "/api/components", c, nil)
}

func (b *httpBackend) UpdateComponent(ctx context.Context, c core.Component) error {
	return b.do(ctx, http.MethodPut, "/api/components/"+url.PathEscape(c.Key), c, nil)
}

func (b *httpBackend) DeleteComponent(ctx context.Context, key string) error {
	return b.do(ctx, http.MethodDelete, "/api/components/"+url.PathEscape(key), nil, nil)
}

func (b *httpBackend) CheckComponent(ctx context.Context, key string) (core.Check, error) {
	var chk core.Check
	return chk, b.do(ctx, http.MethodPost, "/api/components/"+url.PathEscape(key)+"/check", nil, &chk)
}

func (b *httpBackend) HealthSnapshot(ctx context.Context) (core.Health, error) {
	var h core.Health
	return h, b.do(ctx, http.MethodGet, "/api/health", nil, &h)
}
