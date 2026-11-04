package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/moosequest/console/internal/auth"
)

// compile-time assertion that *Store also implements the optional key store.
var _ auth.KeyStore = (*Store)(nil)

// keyTimeLayout is a fixed-width UTC layout (nanosecond precision, always nine
// fractional digits). Unlike RFC3339Nano it does not trim trailing zeros, so
// lexicographic order equals chronological order and `at < ?` and ORDER BY on
// the TEXT columns are correct.
const keyTimeLayout = "2006-01-02T15:04:05.000000000Z"

func fmtKeyTime(t time.Time) string { return t.UTC().Format(keyTimeLayout) }

// nullKeyTime maps the zero time to SQL NULL.
func nullKeyTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return fmtKeyTime(t)
}

func parseKeyTime(s sql.NullString) (time.Time, error) {
	if !s.Valid || s.String == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func parseScope(s string) (auth.Scope, error) {
	for sc := auth.Eval; sc <= auth.Admin; sc++ {
		if sc.String() == s {
			return sc, nil
		}
	}
	return 0, fmt.Errorf("unknown scope %q", s)
}

const keyColumns = `id, name, digest, scope, created_at, expires_at, last_used_at, revoked_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanKey(r rowScanner) (auth.Key, error) {
	var (
		k                      auth.Key
		digest                 []byte
		scope, created         string
		expires, used, revoked sql.NullString
	)
	if err := r.Scan(&k.ID, &k.Name, &digest, &scope, &created, &expires, &used, &revoked); err != nil {
		return auth.Key{}, err
	}
	if len(digest) != sha256.Size {
		return auth.Key{}, fmt.Errorf("key %q: stored digest has %d bytes", k.ID, len(digest))
	}
	copy(k.Digest[:], digest)
	var err error
	if k.Scope, err = parseScope(scope); err != nil {
		return auth.Key{}, fmt.Errorf("key %q: %w", k.ID, err)
	}
	if k.CreatedAt, err = parseKeyTime(sql.NullString{String: created, Valid: true}); err != nil {
		return auth.Key{}, fmt.Errorf("key %q: created_at: %w", k.ID, err)
	}
	if k.ExpiresAt, err = parseKeyTime(expires); err != nil {
		return auth.Key{}, fmt.Errorf("key %q: expires_at: %w", k.ID, err)
	}
	if k.LastUsedAt, err = parseKeyTime(used); err != nil {
		return auth.Key{}, fmt.Errorf("key %q: last_used_at: %w", k.ID, err)
	}
	if k.RevokedAt, err = parseKeyTime(revoked); err != nil {
		return auth.Key{}, fmt.Errorf("key %q: revoked_at: %w", k.ID, err)
	}
	return k, nil
}

// CreateKey implements auth.KeyStore. A name collision returns
// auth.ErrDuplicateName (it wins when several constraints fire, matching
// MemKeyStore); an id or digest collision returns auth.ErrDuplicateKey.
func (s *Store) CreateKey(ctx context.Context, k auth.Key) error {
	if !k.Scope.Valid() {
		return fmt.Errorf("create key: invalid scope %d", int(k.Scope))
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (`+keyColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.Name, k.Digest[:], k.Scope.String(), fmtKeyTime(k.CreatedAt),
		nullKeyTime(k.ExpiresAt), nullKeyTime(k.LastUsedAt), nullKeyTime(k.RevokedAt))
	if err == nil {
		return nil
	}
	if !isUnique(err) {
		return fmt.Errorf("create key: %w", err)
	}
	// SQLite reports only one violated constraint, not necessarily the name
	// one; resolve it so the name always wins.
	var n int
	if qerr := s.rdb.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE name = ?`, k.Name).Scan(&n); qerr == nil && n > 0 {
		return auth.ErrDuplicateName
	}
	return auth.ErrDuplicateKey
}

// LookupKey implements auth.KeyStore.
func (s *Store) LookupKey(ctx context.Context, digest [sha256.Size]byte) (auth.Key, error) {
	k, err := scanKey(s.rdb.QueryRowContext(ctx,
		`SELECT `+keyColumns+` FROM api_keys WHERE digest = ?`, digest[:]))
	if errors.Is(err, sql.ErrNoRows) {
		return auth.Key{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.Key{}, fmt.Errorf("lookup key: %w", err)
	}
	return k, nil
}

// ListKeys implements auth.KeyStore. Digests are zeroed; keys are ordered by
// creation time, then id.
func (s *Store) ListKeys(ctx context.Context) ([]auth.Key, error) {
	rows, err := s.rdb.QueryContext(ctx,
		`SELECT `+keyColumns+` FROM api_keys ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	defer rows.Close()
	out := []auth.Key{}
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, fmt.Errorf("list keys: %w", err)
		}
		k.Digest = [sha256.Size]byte{}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	return out, nil
}

// RevokeKey implements auth.KeyStore. It sets revoked_at only while it is
// NULL, so concurrent revokes of one key yield exactly one success.
func (s *Store) RevokeKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		fmtKeyTime(time.Now()), id)
	if err != nil {
		return fmt.Errorf("revoke key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke key: %w", err)
	}
	if n > 0 {
		return nil
	}
	// No row changed: either the id is unknown or the key was already revoked.
	var exists int
	if err := s.rdb.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE id = ?`, id).Scan(&exists); err != nil {
		return fmt.Errorf("revoke key: %w", err)
	}
	if exists == 0 {
		return auth.ErrNotFound
	}
	return auth.ErrAlreadyRevoked
}

// TouchKey implements auth.KeyStore.
func (s *Store) TouchKey(ctx context.Context, id string, at time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, fmtKeyTime(at), id)
	if err != nil {
		return fmt.Errorf("touch key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("touch key: %w", err)
	}
	if n == 0 {
		return auth.ErrNotFound
	}
	return nil
}

// AppendAudit implements auth.KeyStore.
func (s *Store) AppendAudit(ctx context.Context, e auth.Event) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_events (id, at, principal, action, target, request_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.ID, fmtKeyTime(e.At), e.Principal, e.Action, e.Target, e.RequestID)
	if err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}

// ListAudit implements auth.KeyStore. Events are newest first (ties broken by
// insertion order, newest first); Since is inclusive.
func (s *Store) ListAudit(ctx context.Context, f auth.AuditFilter) ([]auth.Event, error) {
	var (
		q    strings.Builder
		args []any
	)
	q.WriteString(`SELECT id, at, principal, action, target, request_id FROM audit_events`)
	if !f.Since.IsZero() {
		q.WriteString(` WHERE at >= ?`)
		args = append(args, fmtKeyTime(f.Since))
	}
	q.WriteString(` ORDER BY at DESC, rowid DESC`)
	if f.Limit > 0 {
		q.WriteString(` LIMIT ?`)
		args = append(args, f.Limit)
	}
	rows, err := s.rdb.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()
	var out []auth.Event
	for rows.Next() {
		var (
			e  auth.Event
			at string
		)
		if err := rows.Scan(&e.ID, &at, &e.Principal, &e.Action, &e.Target, &e.RequestID); err != nil {
			return nil, fmt.Errorf("list audit: %w", err)
		}
		t, err := parseKeyTime(sql.NullString{String: at, Valid: true})
		if err != nil {
			return nil, fmt.Errorf("list audit: event %q: %w", e.ID, err)
		}
		e.At = t
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	return out, nil
}

// PruneAudit implements auth.KeyStore. It is a single DELETE of events with
// at < before.
func (s *Store) PruneAudit(ctx context.Context, before time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM audit_events WHERE at < ?`, fmtKeyTime(before))
	if err != nil {
		return 0, fmt.Errorf("prune audit: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune audit: %w", err)
	}
	return int(n), nil
}
