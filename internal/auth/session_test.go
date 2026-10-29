package auth

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func TestSessionCSRFTokenRandomPerSession(t *testing.T) {
	s := NewSessionStore(nil)
	var d [32]byte
	a, _ := s.Create(Principal{}, d)
	b, _ := s.Create(Principal{}, d)
	sa, _ := s.Get(a)
	sb, _ := s.Get(b)
	if len(sa.CSRF) != 43 || sa.CSRF == sb.CSRF || sa.CSRF == a {
		t.Errorf("csrf tokens not random/distinct: %q %q", sa.CSRF, sb.CSRF)
	}
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *fakeClock { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }

func TestSessionCreateGet(t *testing.T) {
	st := NewSessionStore(newClock().now)
	d := sha256.Sum256([]byte("tok"))
	id, err := st.Create(Principal{Name: "p", Scope: Read}, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 43 { // 32 bytes, base64url unpadded
		t.Fatalf("id length %d, want 43 (256 bits)", len(id))
	}
	s, ok := st.Get(id)
	if !ok || s.Principal.Name != "p" || s.Digest != d {
		t.Fatalf("Get = %+v, %v", s, ok)
	}
	if _, ok := st.Get("nope"); ok {
		t.Fatal("unknown id accepted")
	}
	st.Delete(id)
	if _, ok := st.Get(id); ok {
		t.Fatal("deleted session still valid")
	}
}

func TestSessionIdleAndAbsoluteExpiry(t *testing.T) {
	clk := newClock()
	st := NewSessionStore(clk.now)
	id, _ := st.Create(Principal{}, [32]byte{})

	// Activity keeps it alive past the idle window...
	for i := 0; i < 20; i++ {
		clk.advance(SessionIdleTimeout - time.Minute)
		if _, ok := st.Get(id); !ok {
			if clk.t.Sub(time.Unix(1_700_000_000, 0)) <= SessionMaxLifetime {
				t.Fatalf("expired early at step %d", i)
			}
			break
		}
	}
	// ...but never past the absolute lifetime.
	if _, ok := st.Get(id); ok {
		t.Fatal("session outlived absolute lifetime")
	}
	if st.Len() != 0 {
		t.Fatal("expired session not purged")
	}

	id2, _ := st.Create(Principal{}, [32]byte{})
	clk.advance(SessionIdleTimeout + time.Second)
	if _, ok := st.Get(id2); ok {
		t.Fatal("idle session accepted")
	}
	if st.Len() != 0 {
		t.Fatal("idle session not purged")
	}
}

func TestSessionStoreBounded(t *testing.T) {
	clk := newClock()
	st := NewSessionStore(clk.now)
	st.max = 5
	var first string
	for i := 0; i < 20; i++ {
		id, _ := st.Create(Principal{}, [32]byte{})
		if i == 0 {
			first = id
		}
		clk.advance(time.Second)
	}
	if st.Len() > 5 {
		t.Fatalf("len %d exceeds bound", st.Len())
	}
	if _, ok := st.Get(first); ok {
		t.Fatal("oldest session should have been evicted")
	}
}

func TestThrottleProgression(t *testing.T) {
	clk := newClock()
	th := NewThrottle(clk.now)
	for i := 0; i < throttleFreeFailures; i++ {
		if d := th.Fail("a"); d != 0 {
			t.Fatalf("failure %d locked out for %v", i+1, d)
		}
		if th.Check("a") != 0 {
			t.Fatal("locked during free failures")
		}
	}
	prev := time.Duration(0)
	for i := 0; i < 5; i++ {
		d := th.Fail("a")
		if d <= prev {
			t.Fatalf("backoff not increasing: %v after %v", d, prev)
		}
		if i > 0 && d != 2*prev {
			t.Fatalf("backoff %v not double %v", d, prev)
		}
		if th.Check("a") != d {
			t.Fatal("Check disagrees with Fail")
		}
		clk.advance(d)
		if th.Check("a") != 0 {
			t.Fatal("still locked after delay elapsed")
		}
		prev = d
	}
	if th.Check("b") != 0 {
		t.Fatal("other client affected")
	}
	for i := 0; i < 40; i++ {
		th.Fail("a")
	}
	if d := th.Fail("a"); d != throttleMax {
		t.Fatalf("delay %v, want cap %v", d, throttleMax)
	}
	th.Success("a")
	if th.Check("a") != 0 || th.Len() != 0 {
		t.Fatal("success should forget the client")
	}
}

func TestThrottleBounded(t *testing.T) {
	clk := newClock()
	th := NewThrottle(clk.now)
	th.max = 10
	for i := 0; i < 100; i++ {
		th.Fail(fmt.Sprintf("c%d", i))
		clk.advance(time.Second)
	}
	if th.Len() > 10 {
		t.Fatalf("len %d exceeds bound", th.Len())
	}
}

func TestStaticAuthenticateDigest(t *testing.T) {
	s := NewStatic(map[Scope][]string{Write: {"w1"}})
	p, err := s.AuthenticateDigest(sha256.Sum256([]byte("w1")))
	if err != nil || p.Scope != Write {
		t.Fatalf("got %+v, %v", p, err)
	}
	if _, err := s.AuthenticateDigest(sha256.Sum256([]byte("x"))); err != ErrUnauthenticated {
		t.Fatalf("got %v", err)
	}
}
