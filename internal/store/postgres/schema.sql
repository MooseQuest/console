-- Schema for the Postgres-backed store. Timestamps are stored as TIMESTAMPTZ in
-- UTC. The flags.variants and components.config columns hold JSONB.

CREATE TABLE IF NOT EXISTS flags (
	key         TEXT PRIMARY KEY,
	description TEXT NOT NULL DEFAULT '',
	enabled     BOOLEAN NOT NULL DEFAULT false,
	scope       TEXT NOT NULL DEFAULT 'all',
	rollout     INT NOT NULL DEFAULT 0,
	variants    JSONB, -- JSON array of core.Variant
	cohort      TEXT NOT NULL DEFAULT '',
	experiment  TEXT NOT NULL DEFAULT '',
	created_at  TIMESTAMPTZ NOT NULL,
	updated_at  TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS components (
	key         TEXT PRIMARY KEY,
	name        TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	provider    TEXT NOT NULL DEFAULT '',
	config      JSONB, -- JSON object of string->string
	created_at  TIMESTAMPTZ NOT NULL,
	updated_at  TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS checks (
	id         BIGSERIAL PRIMARY KEY,
	component  TEXT NOT NULL REFERENCES components(key) ON DELETE CASCADE,
	state      INT NOT NULL,
	message    TEXT NOT NULL DEFAULT '',
	latency    BIGINT NOT NULL DEFAULT 0, -- nanoseconds (time.Duration)
	checked_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS checks_component_time
	ON checks (component, checked_at DESC, id DESC);
