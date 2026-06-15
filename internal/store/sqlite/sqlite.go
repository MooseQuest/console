// Package sqlite provides a SQLite-backed implementation of store.Store. It uses
// the pure-Go modernc.org/sqlite driver (no cgo) so the Console binary stays
// static. Flag variants and component config are persisted as JSON columns;
// timestamps are stored as RFC3339 TEXT in UTC.
package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/store"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Store is a SQLite-backed store.Store. It is safe for concurrent use: the
// underlying *sql.DB serializes access and the busy_timeout pragma absorbs
// transient lock contention.
type Store struct {
	db *sql.DB
}

// compile-time assertion that *Store satisfies the full interface.
var _ store.Store = (*Store)(nil)

// Open opens (or creates) the database at dsn, applies connection pragmas, runs
// the schema migrations, and returns a ready Store. An empty dsn opens a shared
// in-memory database, suitable for tests.
func Open(ctx context.Context, dsn string) (*Store, error) {
	if dsn == "" {
		dsn = "file::memory:?cache=shared"
	}
	dsn = withPragmas(dsn)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// An in-memory database lives only as long as a connection is held; for a
	// shared-cache memory DB, capping the pool to one connection keeps the
	// schema alive for the lifetime of the Store.
	if strings.Contains(dsn, ":memory:") {
		db.SetMaxOpenConns(1)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	return &Store{db: db}, nil
}

// withPragmas appends the busy_timeout and foreign_keys pragmas to dsn,
// preserving any existing query string.
func withPragmas(dsn string) string {
	pragmas := "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	if strings.Contains(dsn, "?") {
		return dsn + "&" + pragmas
	}
	return dsn + "?" + pragmas
}

// Ping verifies the backend is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// isUnique reports whether err is a primary-key/unique constraint violation.
func isUnique(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "PRIMARY KEY")
}

func now(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
}

func fmtTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// --- FlagStore -------------------------------------------------------------

// CreateFlag inserts a new flag. It returns core.ErrConflict if the key exists.
func (s *Store) CreateFlag(ctx context.Context, f core.Flag) error {
	f.CreatedAt = now(f.CreatedAt)
	f.UpdatedAt = now(f.UpdatedAt)

	variants, err := marshalVariants(f.Variants)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO flags (key, description, enabled, scope, rollout, variants, cohort, experiment, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.Key, f.Description, f.Enabled, string(f.Scope), f.Rollout, variants,
		f.Cohort, f.Experiment, fmtTime(f.CreatedAt), fmtTime(f.UpdatedAt))
	if isUnique(err) {
		return fmt.Errorf("flag %q: %w", f.Key, core.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("create flag: %w", err)
	}
	return nil
}

// GetFlag returns the flag with the given key, or core.ErrNotFound.
func (s *Store) GetFlag(ctx context.Context, key string) (core.Flag, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT key, description, enabled, scope, rollout, variants, cohort, experiment, created_at, updated_at
		 FROM flags WHERE key = ?`, key)
	f, err := scanFlag(row)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Flag{}, fmt.Errorf("flag %q: %w", key, core.ErrNotFound)
	}
	if err != nil {
		return core.Flag{}, fmt.Errorf("get flag: %w", err)
	}
	return f, nil
}

// ListFlags returns all flags ordered by key.
func (s *Store) ListFlags(ctx context.Context) ([]core.Flag, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, description, enabled, scope, rollout, variants, cohort, experiment, created_at, updated_at
		 FROM flags ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list flags: %w", err)
	}
	defer rows.Close()

	var out []core.Flag
	for rows.Next() {
		f, err := scanFlag(rows)
		if err != nil {
			return nil, fmt.Errorf("scan flag: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UpdateFlag overwrites the flag with the given key. It returns
// core.ErrNotFound if no such flag exists. CreatedAt is preserved.
func (s *Store) UpdateFlag(ctx context.Context, f core.Flag) error {
	f.UpdatedAt = now(f.UpdatedAt)
	variants, err := marshalVariants(f.Variants)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE flags SET description = ?, enabled = ?, scope = ?, rollout = ?, variants = ?, cohort = ?, experiment = ?, updated_at = ?
		 WHERE key = ?`,
		f.Description, f.Enabled, string(f.Scope), f.Rollout, variants,
		f.Cohort, f.Experiment, fmtTime(f.UpdatedAt), f.Key)
	if err != nil {
		return fmt.Errorf("update flag: %w", err)
	}
	return affectedOrNotFound(res, "flag", f.Key)
}

// DeleteFlag removes the flag with the given key, or returns core.ErrNotFound.
func (s *Store) DeleteFlag(ctx context.Context, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM flags WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("delete flag: %w", err)
	}
	return affectedOrNotFound(res, "flag", key)
}

// scanner abstracts *sql.Row and *sql.Rows for shared scan helpers.
type scanner interface {
	Scan(dest ...any) error
}

func scanFlag(sc scanner) (core.Flag, error) {
	var (
		f        core.Flag
		scope    string
		variants string
		created  string
		updated  string
	)
	if err := sc.Scan(&f.Key, &f.Description, &f.Enabled, &scope, &f.Rollout,
		&variants, &f.Cohort, &f.Experiment, &created, &updated); err != nil {
		return core.Flag{}, err
	}
	f.Scope = core.Scope(scope)
	vs, err := unmarshalVariants(variants)
	if err != nil {
		return core.Flag{}, err
	}
	f.Variants = vs
	if f.CreatedAt, err = parseTime(created); err != nil {
		return core.Flag{}, err
	}
	if f.UpdatedAt, err = parseTime(updated); err != nil {
		return core.Flag{}, err
	}
	return f, nil
}

func marshalVariants(vs []core.Variant) (string, error) {
	if len(vs) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(vs)
	if err != nil {
		return "", fmt.Errorf("marshal variants: %w", err)
	}
	return string(b), nil
}

func unmarshalVariants(s string) ([]core.Variant, error) {
	if s == "" || s == "[]" {
		return nil, nil
	}
	var vs []core.Variant
	if err := json.Unmarshal([]byte(s), &vs); err != nil {
		return nil, fmt.Errorf("unmarshal variants: %w", err)
	}
	if len(vs) == 0 {
		return nil, nil
	}
	return vs, nil
}

// --- ComponentStore --------------------------------------------------------

// CreateComponent inserts a new component, or returns core.ErrConflict.
func (s *Store) CreateComponent(ctx context.Context, c core.Component) error {
	c.CreatedAt = now(c.CreatedAt)
	c.UpdatedAt = now(c.UpdatedAt)
	config, err := marshalConfig(c.Config)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO components (key, name, description, provider, config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.Key, c.Name, c.Description, c.Provider, config,
		fmtTime(c.CreatedAt), fmtTime(c.UpdatedAt))
	if isUnique(err) {
		return fmt.Errorf("component %q: %w", c.Key, core.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("create component: %w", err)
	}
	return nil
}

// GetComponent returns the component with the given key, or core.ErrNotFound.
func (s *Store) GetComponent(ctx context.Context, key string) (core.Component, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT key, name, description, provider, config, created_at, updated_at
		 FROM components WHERE key = ?`, key)
	c, err := scanComponent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Component{}, fmt.Errorf("component %q: %w", key, core.ErrNotFound)
	}
	if err != nil {
		return core.Component{}, fmt.Errorf("get component: %w", err)
	}
	return c, nil
}

// ListComponents returns all components ordered by key.
func (s *Store) ListComponents(ctx context.Context) ([]core.Component, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, name, description, provider, config, created_at, updated_at
		 FROM components ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list components: %w", err)
	}
	defer rows.Close()

	var out []core.Component
	for rows.Next() {
		c, err := scanComponent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan component: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateComponent overwrites the component with the given key, or returns
// core.ErrNotFound. CreatedAt is preserved.
func (s *Store) UpdateComponent(ctx context.Context, c core.Component) error {
	c.UpdatedAt = now(c.UpdatedAt)
	config, err := marshalConfig(c.Config)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE components SET name = ?, description = ?, provider = ?, config = ?, updated_at = ?
		 WHERE key = ?`,
		c.Name, c.Description, c.Provider, config, fmtTime(c.UpdatedAt), c.Key)
	if err != nil {
		return fmt.Errorf("update component: %w", err)
	}
	return affectedOrNotFound(res, "component", c.Key)
}

// DeleteComponent removes the component with the given key (and, via the
// foreign key, its checks), or returns core.ErrNotFound.
func (s *Store) DeleteComponent(ctx context.Context, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM components WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("delete component: %w", err)
	}
	return affectedOrNotFound(res, "component", key)
}

func scanComponent(sc scanner) (core.Component, error) {
	var (
		c       core.Component
		config  string
		created string
		updated string
	)
	if err := sc.Scan(&c.Key, &c.Name, &c.Description, &c.Provider, &config,
		&created, &updated); err != nil {
		return core.Component{}, err
	}
	cfg, err := unmarshalConfig(config)
	if err != nil {
		return core.Component{}, err
	}
	c.Config = cfg
	if c.CreatedAt, err = parseTime(created); err != nil {
		return core.Component{}, err
	}
	if c.UpdatedAt, err = parseTime(updated); err != nil {
		return core.Component{}, err
	}
	return c, nil
}

func marshalConfig(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}
	return string(b), nil
}

func unmarshalConfig(s string) (map[string]string, error) {
	if s == "" || s == "{}" {
		return nil, nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if len(m) == 0 {
		return nil, nil
	}
	return m, nil
}

// --- CheckStore ------------------------------------------------------------

// RecordCheck appends a health-check observation.
func (s *Store) RecordCheck(ctx context.Context, c core.Check) error {
	c.CheckedAt = now(c.CheckedAt)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO checks (component, state, message, latency, checked_at)
		 VALUES (?, ?, ?, ?, ?)`,
		c.Component, int(c.State), c.Message, int64(c.Latency), fmtTime(c.CheckedAt))
	if err != nil {
		return fmt.Errorf("record check: %w", err)
	}
	return nil
}

// LatestCheck returns the most recent check for the given component, or
// core.ErrNotFound if it has none.
func (s *Store) LatestCheck(ctx context.Context, component string) (core.Check, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT component, state, message, latency, checked_at
		 FROM checks WHERE component = ?
		 ORDER BY checked_at DESC, id DESC LIMIT 1`, component)
	c, err := scanCheck(row)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Check{}, fmt.Errorf("check for %q: %w", component, core.ErrNotFound)
	}
	if err != nil {
		return core.Check{}, fmt.Errorf("latest check: %w", err)
	}
	return c, nil
}

// LatestChecks returns the most recent check for each component, one row per
// component, ordered by component key.
func (s *Store) LatestChecks(ctx context.Context) ([]core.Check, error) {
	// Pick the row with the greatest id among the rows tied for the latest
	// checked_at per component; id is monotonic so it breaks ties correctly.
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.component, c.state, c.message, c.latency, c.checked_at
		 FROM checks c
		 JOIN (
			SELECT component, MAX(id) AS id
			FROM checks
			WHERE id IN (
				SELECT id FROM checks ci
				WHERE ci.checked_at = (
					SELECT MAX(checked_at) FROM checks cm WHERE cm.component = ci.component
				)
			)
			GROUP BY component
		 ) latest ON latest.id = c.id
		 ORDER BY c.component`)
	if err != nil {
		return nil, fmt.Errorf("latest checks: %w", err)
	}
	defer rows.Close()

	var out []core.Check
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			return nil, fmt.Errorf("scan check: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanCheck(sc scanner) (core.Check, error) {
	var (
		c       core.Check
		state   int
		latency int64
		checked string
	)
	if err := sc.Scan(&c.Component, &state, &c.Message, &latency, &checked); err != nil {
		return core.Check{}, err
	}
	c.State = core.HealthState(state)
	c.Latency = time.Duration(latency)
	var err error
	if c.CheckedAt, err = parseTime(checked); err != nil {
		return core.Check{}, err
	}
	return c, nil
}

// --- helpers ---------------------------------------------------------------

// affectedOrNotFound returns core.ErrNotFound when res affected no rows.
func affectedOrNotFound(res sql.Result, kind, key string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%s %q: %w", kind, key, core.ErrNotFound)
	}
	return nil
}
