package obs

import (
	"context"
	"errors"
	"time"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/store"
)

// Store metric names.
const (
	// StoreOpDuration is the per-operation latency histogram, labelled op.
	StoreOpDuration = "console_store_op_duration_seconds"
	// StoreErrors counts failed operations, labelled op. Not-found and
	// conflict sentinels are expected outcomes and are not counted.
	StoreErrors = "console_store_errors_total"
)

// Compile-time interface assertion.
var _ store.Store = (*instrumentedStore)(nil)

// storeOps is the fixed set of op label values: the store.Store method names.
var storeOps = [...]string{
	"CreateFlag", "GetFlag", "ListFlags", "UpdateFlag", "DeleteFlag",
	"CreateComponent", "GetComponent", "ListComponents", "UpdateComponent", "DeleteComponent",
	"RecordCheck", "LatestCheck", "LatestChecks",
	"Ping", "Close",
}

type storeOp struct {
	dur  Histogram
	errs Counter
}

type instrumentedStore struct {
	next store.Store
	ops  map[string]storeOp // resolved once; read-only after construction
}

// WrapStore returns a store.Store that records latency and error counts for
// every method on rec, delegating to next. The op label is the method name.
// core.ErrNotFound and core.ErrConflict are not counted as errors. Instruments
// are resolved once here, so the per-call path does no label lookups.
func WrapStore(next store.Store, rec Recorder) store.Store {
	if rec == nil {
		rec = Nop
	}
	ops := make(map[string]storeOp, len(storeOps))
	for _, op := range storeOps {
		ops[op] = storeOp{
			dur:  rec.Histogram(StoreOpDuration, L("op", op)),
			errs: rec.Counter(StoreErrors, L("op", op)),
		}
	}
	return &instrumentedStore{next: next, ops: ops}
}

// done records the outcome of op and returns err unchanged.
func (s *instrumentedStore) done(op string, start time.Time, err error) error {
	o := s.ops[op]
	o.dur.Observe(time.Since(start).Seconds())
	if err != nil && !errors.Is(err, core.ErrNotFound) && !errors.Is(err, core.ErrConflict) {
		o.errs.Inc()
	}
	return err
}

func (s *instrumentedStore) CreateFlag(ctx context.Context, f core.Flag) error {
	t := time.Now()
	return s.done("CreateFlag", t, s.next.CreateFlag(ctx, f))
}

func (s *instrumentedStore) GetFlag(ctx context.Context, key string) (core.Flag, error) {
	t := time.Now()
	f, err := s.next.GetFlag(ctx, key)
	return f, s.done("GetFlag", t, err)
}

func (s *instrumentedStore) ListFlags(ctx context.Context) ([]core.Flag, error) {
	t := time.Now()
	fs, err := s.next.ListFlags(ctx)
	return fs, s.done("ListFlags", t, err)
}

func (s *instrumentedStore) UpdateFlag(ctx context.Context, f core.Flag) error {
	t := time.Now()
	return s.done("UpdateFlag", t, s.next.UpdateFlag(ctx, f))
}

func (s *instrumentedStore) DeleteFlag(ctx context.Context, key string) error {
	t := time.Now()
	return s.done("DeleteFlag", t, s.next.DeleteFlag(ctx, key))
}

func (s *instrumentedStore) CreateComponent(ctx context.Context, c core.Component) error {
	t := time.Now()
	return s.done("CreateComponent", t, s.next.CreateComponent(ctx, c))
}

func (s *instrumentedStore) GetComponent(ctx context.Context, key string) (core.Component, error) {
	t := time.Now()
	c, err := s.next.GetComponent(ctx, key)
	return c, s.done("GetComponent", t, err)
}

func (s *instrumentedStore) ListComponents(ctx context.Context) ([]core.Component, error) {
	t := time.Now()
	cs, err := s.next.ListComponents(ctx)
	return cs, s.done("ListComponents", t, err)
}

func (s *instrumentedStore) UpdateComponent(ctx context.Context, c core.Component) error {
	t := time.Now()
	return s.done("UpdateComponent", t, s.next.UpdateComponent(ctx, c))
}

func (s *instrumentedStore) DeleteComponent(ctx context.Context, key string) error {
	t := time.Now()
	return s.done("DeleteComponent", t, s.next.DeleteComponent(ctx, key))
}

func (s *instrumentedStore) RecordCheck(ctx context.Context, c core.Check) error {
	t := time.Now()
	return s.done("RecordCheck", t, s.next.RecordCheck(ctx, c))
}

func (s *instrumentedStore) LatestCheck(ctx context.Context, component string) (core.Check, error) {
	t := time.Now()
	c, err := s.next.LatestCheck(ctx, component)
	return c, s.done("LatestCheck", t, err)
}

func (s *instrumentedStore) LatestChecks(ctx context.Context) ([]core.Check, error) {
	t := time.Now()
	cs, err := s.next.LatestChecks(ctx)
	return cs, s.done("LatestChecks", t, err)
}

func (s *instrumentedStore) Ping(ctx context.Context) error {
	t := time.Now()
	return s.done("Ping", t, s.next.Ping(ctx))
}

func (s *instrumentedStore) Close() error {
	t := time.Now()
	return s.done("Close", t, s.next.Close())
}
