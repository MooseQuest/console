package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moosequest/console/internal/auth"
	"github.com/moosequest/console/internal/config"
)

var base = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

func seed(t *testing.T, ks auth.KeyStore) {
	t.Helper()
	for i, age := range []time.Duration{100 * 24 * time.Hour, 91 * 24 * time.Hour, 89 * 24 * time.Hour, time.Hour} {
		e := auth.Event{ID: string(rune('a' + i)), At: base.Add(-age), Principal: "p", Action: auth.ActionKeyCreate, Target: "k"}
		if err := ks.AppendAudit(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPruneOnceDeletesOldAndRecordsEvent(t *testing.T) {
	ks := auth.NewMemKeyStore()
	seed(t, ks)
	pruneOnce(context.Background(), ks, 90*24*time.Hour, func() time.Time { return base })

	ev, _ := ks.ListAudit(context.Background(), auth.AuditFilter{})
	if len(ev) != 3 {
		t.Fatalf("want 2 retained + 1 prune event, got %+v", ev)
	}
	if ev[0].Action != auth.ActionAuditPrune || ev[0].Principal != "system" || !ev[0].At.Equal(base) || len(ev[0].ID) != 32 {
		t.Fatalf("prune event: %+v", ev[0])
	}
}

type failingKS struct{ auth.KeyStore }

func (failingKS) PruneAudit(context.Context, time.Time) (int, error) {
	return 0, errors.New("boom")
}

func TestPruneOnceFailureDoesNotPanicOrRecord(t *testing.T) {
	mem := auth.NewMemKeyStore()
	pruneOnce(context.Background(), failingKS{mem}, time.Hour, time.Now)
	if ev, _ := mem.ListAudit(context.Background(), auth.AuditFilter{}); len(ev) != 0 {
		t.Fatalf("no event on failed prune: %+v", ev)
	}
}

func TestRunPruneLoopStartupThenTicksThenStops(t *testing.T) {
	ks := auth.NewMemKeyStore()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runPruneLoop(ctx, ks, time.Hour, 10*time.Millisecond, time.Now)
	}()
	deadline := time.After(5 * time.Second)
	for {
		ev, _ := ks.ListAudit(context.Background(), auth.AuditFilter{})
		if len(ev) >= 3 { // startup pass + at least two ticks
			break
		}
		select {
		case <-deadline:
			t.Fatalf("loop did not tick, events: %d", len(ev))
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not stop on context cancel")
	}
}

func TestRunAuditPrunerSkippedWhenForeverOrNoKeyStore(t *testing.T) {
	ks := auth.NewMemKeyStore()
	seed(t, ks)
	for name, a := range map[string]*App{
		"forever":  {Config: config.Config{AuditRetentionDays: 0}, Keys: ks},
		"no store": {Config: config.Config{AuditRetentionDays: 90}},
	} {
		done := make(chan struct{})
		go func() { defer close(done); a.RunAuditPruner(context.Background()) }()
		select {
		case <-done: // returns immediately instead of blocking
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: RunAuditPruner must return immediately", name)
		}
	}
	if ev, _ := ks.ListAudit(context.Background(), auth.AuditFilter{}); len(ev) != 4 {
		t.Fatalf("nothing may be pruned or recorded: %+v", ev)
	}
}

func TestRunAuditPrunerPrunesSQLiteOnStartup(t *testing.T) {
	cfg := config.Default()
	cfg.DB = ":memory:"
	a, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Keys == nil {
		t.Fatal("SQLite store must provide Keys")
	}
	old := auth.Event{ID: "old", At: time.Now().Add(-120 * 24 * time.Hour), Principal: "p", Action: auth.ActionKeyCreate}
	recent := auth.Event{ID: "new", At: time.Now().Add(-24 * time.Hour), Principal: "p", Action: auth.ActionKeyCreate}
	for _, e := range []auth.Event{old, recent} {
		if err := a.Keys.AppendAudit(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); a.RunAuditPruner(ctx) }()
	deadline := time.After(5 * time.Second)
	for {
		ev, _ := a.Keys.ListAudit(context.Background(), auth.AuditFilter{})
		if len(ev) == 2 && ev[0].Action == auth.ActionAuditPrune && ev[1].ID == "new" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("startup prune not observed: %+v", ev)
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done
}
