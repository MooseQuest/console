// Package postgres provides a Postgres-backed implementation of store.Store. It
// uses the pure-Go github.com/jackc/pgx/v5 driver (no cgo) via database/sql, so
// the Console binary stays static. Flag variants and component config are
// persisted as JSONB columns; timestamps are stored as TIMESTAMPTZ in UTC.
package postgres

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/store"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed schema.sql
var schema string

// Store is a Postgres-backed store.Store. It is safe for concurrent use: the
// underlying *sql.DB manages a connection pool.
type Store struct {
	db *sql.DB
}

// compile-time assertion that *Store satisfies the full interface.
var _ store.Store = (*Store)(nil)

// Open connects to the Postgres database at dsn, sets sane pool limits, verifies
// connectivity, runs the idempotent schema migrations, and returns a ready
// Store.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Ping verifies the backend is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// isUnique reports whether err is a primary-key/unique constraint violation
// (Postgres SQLSTATE 23505).
func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	return false
}

func now(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		f.Key, f.Description, f.Enabled, string(f.Scope), f.Rollout, variants,
		f.Cohort, f.Experiment, f.CreatedAt, f.UpdatedAt)
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
		 FROM flags WHERE key = $1`, key)
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
		`UPDATE flags SET description = $1, enabled = $2, scope = $3, rollout = $4, variants = $5, cohort = $6, experiment = $7, updated_at = $8
		 WHERE key = $9`,
		f.Description, f.Enabled, string(f.Scope), f.Rollout, variants,
		f.Cohort, f.Experiment, f.UpdatedAt, f.Key)
	if err != nil {
		return fmt.Errorf("update flag: %w", err)
	}
	return affectedOrNotFound(res, "flag", f.Key)
}

// DeleteFlag removes the flag with the given key, or returns core.ErrNotFound.
func (s *Store) DeleteFlag(ctx context.Context, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM flags WHERE key = $1`, key)
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
		variants []byte
	)
	if err := sc.Scan(&f.Key, &f.Description, &f.Enabled, &scope, &f.Rollout,
		&variants, &f.Cohort, &f.Experiment, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return core.Flag{}, err
	}
	f.Scope = core.Scope(scope)
	vs, err := unmarshalVariants(variants)
	if err != nil {
		return core.Flag{}, err
	}
	f.Variants = vs
	f.CreatedAt = f.CreatedAt.UTC()
	f.UpdatedAt = f.UpdatedAt.UTC()
	return f, nil
}

func marshalVariants(vs []core.Variant) ([]byte, error) {
	if len(vs) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(vs)
	if err != nil {
		return nil, fmt.Errorf("marshal variants: %w", err)
	}
	return b, nil
}

func unmarshalVariants(b []byte) ([]core.Variant, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var vs []core.Variant
	if err := json.Unmarshal(b, &vs); err != nil {
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		c.Key, c.Name, c.Description, c.Provider, config,
		c.CreatedAt, c.UpdatedAt)
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
		 FROM components WHERE key = $1`, key)
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
		`UPDATE components SET name = $1, description = $2, provider = $3, config = $4, updated_at = $5
		 WHERE key = $6`,
		c.Name, c.Description, c.Provider, config, c.UpdatedAt, c.Key)
	if err != nil {
		return fmt.Errorf("update component: %w", err)
	}
	return affectedOrNotFound(res, "component", c.Key)
}

// DeleteComponent removes the component with the given key (and, via the
// foreign key, its checks), or returns core.ErrNotFound.
func (s *Store) DeleteComponent(ctx context.Context, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM components WHERE key = $1`, key)
	if err != nil {
		return fmt.Errorf("delete component: %w", err)
	}
	return affectedOrNotFound(res, "component", key)
}

func scanComponent(sc scanner) (core.Component, error) {
	var (
		c      core.Component
		config []byte
	)
	if err := sc.Scan(&c.Key, &c.Name, &c.Description, &c.Provider, &config,
		&c.CreatedAt, &c.UpdatedAt); err != nil {
		return core.Component{}, err
	}
	cfg, err := unmarshalConfig(config)
	if err != nil {
		return core.Component{}, err
	}
	c.Config = cfg
	c.CreatedAt = c.CreatedAt.UTC()
	c.UpdatedAt = c.UpdatedAt.UTC()
	return c, nil
}

func marshalConfig(m map[string]string) ([]byte, error) {
	if len(m) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	return b, nil
}

func unmarshalConfig(b []byte) (map[string]string, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
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
		 VALUES ($1, $2, $3, $4, $5)`,
		c.Component, int(c.State), c.Message, int64(c.Latency), c.CheckedAt)
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
		 FROM checks WHERE component = $1
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
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT ON (component) component, state, message, latency, checked_at
		 FROM checks
		 ORDER BY component, checked_at DESC, id DESC`)
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
	)
	if err := sc.Scan(&c.Component, &state, &c.Message, &latency, &c.CheckedAt); err != nil {
		return core.Check{}, err
	}
	c.State = core.HealthState(state)
	c.Latency = time.Duration(latency)
	c.CheckedAt = c.CheckedAt.UTC()
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
