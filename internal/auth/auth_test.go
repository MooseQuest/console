package auth

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func req(h string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	if h != "" {
		r.Header.Set("Authorization", h)
	}
	return r
}

func TestScopeHierarchy(t *testing.T) {
	all := []Scope{Eval, Read, Write, Admin}
	for i, have := range all {
		for j, need := range all {
			if got, want := have.Allows(need), i >= j; got != want {
				t.Errorf("%s.Allows(%s)=%v want %v", have, need, got, want)
			}
		}
	}
	if Scope(0).Allows(Eval) || Admin.Allows(Scope(0)) || Scope(99).Allows(Eval) {
		t.Error("invalid scope must allow nothing")
	}
}

func TestStaticAuthenticate(t *testing.T) {
	s := NewStatic(map[Scope][]string{
		Admin: {"adm-1"},
		Read:  {"rd-1", " ", "rd-2"},
	})
	p, err := s.Authenticate(req("Bearer rd-2"))
	if err != nil || p.Scope != Read || !strings.HasPrefix(p.Name, "read-2-") {
		t.Fatalf("got %+v, %v", p, err)
	}
	if p, err = s.Authenticate(req("bearer adm-1")); err != nil || p.Scope != Admin {
		t.Fatalf("got %+v, %v", p, err)
	}
}

func TestStaticBadInputs(t *testing.T) {
	s := NewStatic(map[Scope][]string{Write: {"secret-token-value"}})
	for _, h := range []string{"", "Bearer", "Bearer ", "Basic secret-token-value", "secret-token-value", "Bearer nope", "Bearer secret-token-valu"} {
		p, err := s.Authenticate(req(h))
		if !errors.Is(err, ErrUnauthenticated) || p != (Principal{}) {
			t.Errorf("%q: got %+v, %v", h, p, err)
		}
		if err != nil && strings.Contains(err.Error(), "nope") {
			t.Errorf("error echoes credential: %v", err)
		}
	}
	if _, err := NewStatic(nil).Authenticate(req("Bearer x")); !errors.Is(err, ErrUnauthenticated) {
		t.Error("empty authenticator must reject")
	}
}

func TestNeverLeaksToken(t *testing.T) {
	const tok = "csk_supersecrettokenvalue1234567890"
	body := strings.TrimPrefix(tok, TokenPrefix)
	s := NewStatic(map[Scope][]string{Write: {tok}})
	p, err := s.Authenticate(req("Bearer " + tok))
	if err != nil {
		t.Fatal(err)
	}
	// No 4+ character window of the token body may appear in the name.
	for i := 0; i+4 <= len(body); i++ {
		if strings.Contains(p.Name, body[i:i+4]) {
			t.Fatalf("principal name %q contains token fragment %q", p.Name, body[i:i+4])
		}
	}
	for _, h := range []string{"Bearer " + tok + "x", "Bearer " + body} {
		_, err = s.Authenticate(req(h))
		if err == nil || strings.Contains(err.Error(), body) {
			t.Fatalf("bad error: %v", err)
		}
	}
}

func TestDuplicateResolvesToHighest(t *testing.T) {
	s := NewStatic(map[Scope][]string{Read: {"dup"}, Admin: {"dup"}, Write: {"dup"}})
	p, err := s.Authenticate(req("Bearer dup"))
	if err != nil || p.Scope != Admin {
		t.Fatalf("got %+v, %v", p, err)
	}
	if len(s.entries) != 1 {
		t.Errorf("entries=%d want 1", len(s.entries))
	}
}

func TestGenerateToken(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GenerateToken()
	if a == b {
		t.Error("tokens must differ")
	}
	if !strings.HasPrefix(a, "csk_") {
		t.Errorf("missing prefix: %q", a)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(a, "csk_"))
	if err != nil || len(raw) != 32 {
		t.Errorf("want 32 base64url bytes, got %d, %v", len(raw), err)
	}
}
