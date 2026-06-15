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
