package notify

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/obs"
)

func TestDispatcher_RecordsOutcomes(t *testing.T) {
	good := &fakeNotifier{name: "slack"}
	bad := &fakeNotifier{name: "hook", err: errors.New("boom https://example.com/secret-webhook")}
	d := NewDispatcher(bad, good)
	d.logf = func(string, ...any) {}
	mem := obs.NewMemory()
	d.SetRecorder(mem)

	d.Emit(core.Event{})

	if good.count() != 1 {
		t.Fatal("failing notifier blocked the others")
	}
	var buf bytes.Buffer
	if err := mem.WritePrometheus(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`console_notify_sent_total{outcome="ok",sink="slack"} 1`,
		`console_notify_sent_total{outcome="error",sink="hook"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "example.com") || strings.Contains(out, "secret") {
		t.Errorf("metrics leaked sensitive data:\n%s", out)
	}
}

func TestDispatcher_NilRecorderIsNop(t *testing.T) {
	d := NewDispatcher(&fakeNotifier{name: "a"})
	d.SetRecorder(nil)
	d.Emit(core.Event{})
}

// fakeNotifier records the events it receives and can be made to fail.
type fakeNotifier struct {
	name string
	err  error
	mu   sync.Mutex
	got  []core.Event
}

func (f *fakeNotifier) Name() string { return f.name }

func (f *fakeNotifier) Notify(_ context.Context, ev core.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, ev)
	return f.err
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

func TestDispatcher_FansOutToAll(t *testing.T) {
	a := &fakeNotifier{name: "a"}
	b := &fakeNotifier{name: "b"}
	d := NewDispatcher(a)
	d.Register(b)

	if d.Len() != 2 {
		t.Fatalf("Len = %d, want 2", d.Len())
	}
	d.Emit(core.Event{Type: core.EventComponentDown, Title: "x down"})

	if a.count() != 1 || b.count() != 1 {
		t.Fatalf("each notifier should get 1 event, got a=%d b=%d", a.count(), b.count())
	}
}

func TestDispatcher_ErrorIsLoggedNotPropagated(t *testing.T) {
	var logged int
	bad := &fakeNotifier{name: "bad", err: errors.New("boom")}
	good := &fakeNotifier{name: "good"}
	d := NewDispatcher(bad, good)
	d.logf = func(string, ...any) { logged++ }

	// Emit returns nothing and must not panic; the good notifier still fires.
	d.Emit(core.Event{Type: core.EventFlagChanged, Title: "flag x"})

	if good.count() != 1 {
		t.Errorf("good notifier should still receive the event")
	}
	if logged != 1 {
		t.Errorf("expected 1 logged error, got %d", logged)
	}
}

func TestDispatcher_NoNotifiers(t *testing.T) {
	d := NewDispatcher()
	if d.Len() != 0 {
		t.Fatalf("expected empty dispatcher")
	}
	d.Emit(core.Event{Type: core.EventComponentDown}) // must be a safe no-op
}
