package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/moosequest/console/internal/core"
)

// httpBackend is the -addr Backend: it drives a running `console serve` over its
// JSON API. It lets the MCP server target a shared or remote Console instead of
// opening the local store. The remote endpoint has no built-in auth, so this
// mode should point at a loopback address or an authenticating proxy (see the
// runtime-hardening SOP).
type httpBackend struct {
	base string
	http *http.Client
}

// NewHTTPBackend builds a Backend that talks to the Console JSON API at addr.
// addr may be a bare host:port ("127.0.0.1:8080") or a full URL; a missing
// scheme defaults to http.
func NewHTTPBackend(addr string) (Backend, error) {
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
	base := strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/")
	return &httpBackend{base: base, http: &http.Client{Timeout: 30 * time.Second}}, nil
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
	resp, err := b.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
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
