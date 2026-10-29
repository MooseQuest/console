package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
)

// RequestIDHeader is the header used to accept and echo the request ID.
const RequestIDHeader = "X-Request-Id"

// maxRequestIDLen bounds an accepted inbound request ID.
const maxRequestIDLen = 64

type requestIDKey struct{}

// RequestID returns the request ID stored in ctx by the request-ID
// middleware, or "" if there is none. The auth audit log consumes it.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// validRequestID reports whether id is 1..64 chars of [A-Za-z0-9._-].
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// newRequestID returns 128 random bits as hex.
func newRequestID() string {
	var b [16]byte
	_, _ = io.ReadFull(rand.Reader, b[:])
	return hex.EncodeToString(b[:])
}

// requestID accepts a well-formed inbound X-Request-Id or generates one, sets
// it on the response and stores it in the request context. Because the context
// swap makes a request copy, the matched route pattern the mux records on the
// copy is propagated back so outer middleware (metrics) still sees it.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		r2 := r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		defer func() { r.Pattern = r2.Pattern }()
		next.ServeHTTP(w, r2)
	})
}

// NewLogger builds the process logger writing to w in the given format
// ("json" for JSON lines, anything else text). Validate the format with
// config.Config.Validate first.
func NewLogger(w io.Writer, format string) *slog.Logger {
	if format == "json" {
		return slog.New(slog.NewJSONHandler(w, nil))
	}
	return slog.New(slog.NewTextHandler(w, nil))
}
