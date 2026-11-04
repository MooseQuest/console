-- Schema for the SQLite-backed store. Timestamps are stored as RFC3339 TEXT in
-- UTC. The flags.variants and components.config columns hold JSON.

CREATE TABLE IF NOT EXISTS flags (
	key         TEXT PRIMARY KEY,
	description TEXT NOT NULL DEFAULT '',
	enabled     INTEGER NOT NULL DEFAULT 0,
	scope       TEXT NOT NULL DEFAULT '',
	rollout     INTEGER NOT NULL DEFAULT 0,
	variants    TEXT NOT NULL DEFAULT '[]', -- JSON array of core.Variant
	cohort      TEXT NOT NULL DEFAULT '',
	experiment  TEXT NOT NULL DEFAULT '',
	created_at  TEXT NOT NULL,
	updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS components (
	key         TEXT PRIMARY KEY,
	name        TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	provider    TEXT NOT NULL DEFAULT '',
	config      TEXT NOT NULL DEFAULT '{}', -- JSON object of string->string
	created_at  TEXT NOT NULL,
	updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS checks (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	component  TEXT NOT NULL,
	state      INTEGER NOT NULL,
	message    TEXT NOT NULL DEFAULT '',
	latency    INTEGER NOT NULL DEFAULT 0, -- nanoseconds (time.Duration)
	checked_at TEXT NOT NULL,
	FOREIGN KEY (component) REFERENCES components(key) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_checks_component_checked_at
	ON checks (component, checked_at DESC, id DESC);

-- Managed API keys and the key-management audit trail (docs/design/auth.md
-- section 5). These statements are idempotent, so they run on every open and
-- are a no-op on a database that already has them; older binaries ignore the
-- extra tables. Only the SHA-256 digest of a token is stored. Timestamps use
-- a fixed-width UTC format (see fmtKeyTime) so TEXT comparison and ORDER BY
-- agree with chronological order.
CREATE TABLE IF NOT EXISTS api_keys (
	id           TEXT PRIMARY KEY,
	name         TEXT NOT NULL UNIQUE,
	digest       BLOB NOT NULL UNIQUE, -- sha256 of the token
	scope        TEXT NOT NULL,        -- eval|read|write|admin
	created_at   TEXT NOT NULL,
	expires_at   TEXT,
	last_used_at TEXT,
	revoked_at   TEXT
);

CREATE TABLE IF NOT EXISTS audit_events (
	id         TEXT PRIMARY KEY,
	at         TEXT NOT NULL,
	principal  TEXT NOT NULL,
	action     TEXT NOT NULL,
	target     TEXT NOT NULL,
	request_id TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS audit_events_at ON audit_events (at);
