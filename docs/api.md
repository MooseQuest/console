# HTTP API reference

Console exposes a JSON HTTP API over the flag and status engines. The dashboard
is just a client of this same API, and it is how your applications integrate
today — there is **no native client SDK yet** (it's on the roadmap; integrate via
HTTP for now).

- [Conventions](#conventions)
- [Authentication and scopes](#authentication-and-scopes)
- [Errors](#errors)
- [Health](#health)
- [Flags](#flags)
- [Components](#components)
- [Integrating from app code](#integrating-from-app-code)

## Conventions

- Base URL defaults to `http://localhost:8080` (`CONSOLE_ADDR`, default
  `127.0.0.1:8080` — loopback).
- All request and response bodies are JSON. Send `Content-Type: application/json`
  on writes.
- Resources are addressed by their `key`.

### Security

- **Authenticated by default once a token is configured.** See
  [Authentication and scopes](#authentication-and-scopes). `console serve`
  refuses to bind a non-loopback address without a token.
- **Request bodies are capped at 1 MiB.** An over-limit body is rejected rather
  than buffered.
- The server sets security headers on every response: `X-Content-Type-Options:
  nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, and a
  restrictive Content-Security-Policy.

| Method | Path | Scope | Description |
|---|---|---|---|
| `GET` | `/healthz` | none | Liveness probe (returns `ok`) |
| `GET` | `/api/health` | `read` | Aggregate health snapshot |
| `GET` | `/api/flags` | `read` | List flags |
| `POST` | `/api/flags` | `write` | Create a flag |
| `GET` | `/api/flags/{key}` | `read` | Get a flag |
| `PUT` | `/api/flags/{key}` | `write` | Update a flag |
| `DELETE` | `/api/flags/{key}` | `write` | Delete a flag |
| `POST` | `/api/flags/{key}/evaluate` | `eval` | Evaluate a flag for a subject |
| `GET` | `/api/components` | `read` | List components |
| `POST` | `/api/components` | `write` | Create a component |
| `GET` | `/api/components/{key}` | `read` | Get a component |
| `PUT` | `/api/components/{key}` | `write` | Update a component |
| `DELETE` | `/api/components/{key}` | `write` | Delete a component |
| `POST` | `/api/components/{key}/check` | `write` | Run a check now |
| `POST` | `/api/auth/keys` | `admin` | Create a managed key (token shown once) |
| `GET` | `/api/auth/keys` | `admin` | List managed keys |
| `DELETE` | `/api/auth/keys/{id}` | `admin` | Revoke a managed key |
| `GET` | `/api/auth/audit` | `admin` | List key audit events |

## Authentication and scopes

When at least one token is configured (`CONSOLE_ADMIN_TOKENS`,
`CONSOLE_WRITE_TOKENS`, `CONSOLE_READ_TOKENS`, `CONSOLE_EVAL_TOKENS`, or the
`_FILE` variants), every endpoint except `/healthz` requires a bearer token:

```bash
export CONSOLE_TOKEN=$(console auth token)   # generate once; configure it on the server
curl -H "Authorization: Bearer $CONSOLE_TOKEN" localhost:8080/api/flags
```

Scopes are hierarchical, `admin` > `write` > `read` > `eval`: a token can call
any endpoint that needs its scope or a lower one. The table above lists the
scope each endpoint needs. Use an `eval` token in application code (it can only
evaluate flags), a `read` token for dashboards, monitors and Prometheus, and a
`write` token for automation that changes state. The dashboard routes (`/`,
`/flags`, `/status`) need `read`, and its toggle and check actions need `write`.

- `GET /healthz` is the only unauthenticated API route. `GET /api/health` now
  needs `read`; point probes at `/healthz`.
- `GET /metrics` needs `read` (see [observability](observability.md)).
- Browsers sign in at `/login` with a token and get a session cookie; that path
  is for the dashboard. API clients should send the `Authorization` header,
  which is never combined with the cookie and needs no CSRF token.
- With no token configured, Console runs open, but only on a loopback bind.

### Managed API keys (`console auth key`)

Besides the environment tokens, SQLite deployments can store keys, which can
be created, expired and revoked without a restart:

```bash
console auth key create ci-bot -scope read -expires-in 90d   # prints the token once
console auth key list [-json]                                  # never shows digests
console auth key revoke ci-bot                                 # by id or by name
console auth audit list [-since 7d] [-limit 50] [-json]
console auth audit prune [-older-than 90d | -all]
```

- `create` prints the plaintext token to stdout exactly once and stores only its
  SHA-256 digest; lose it and you must create a new key. `-scope` is required;
  `-expires-in` takes Go durations or whole days (`36h`, `90d`) and must be
  positive.
- `revoke` resolves its argument as a key id first, then as a name. The server
  re-checks the key store on every request, so revocation takes effect on the
  next request, provided the CLI and the server share the same SQLite file
  (the same `CONSOLE_DB`).
- Key creation, revocation and pruning are recorded as audit events with
  principal `cli`; the target is the key id (or, for `audit.prune`, the number
  of events deleted).
- `audit prune` with no flag deletes events older than
  `CONSOLE_AUDIT_RETENTION_DAYS` (default 90). If that is `0` (keep forever) it
  refuses and asks for `-older-than`. `-older-than 0` is refused unless `-all`,
  and `-all` cannot be combined with a positive `-older-than`.
- Backends without a key store (plugin stores) cannot use these commands; use
  the `CONSOLE_*_TOKENS` variables.
- Exit codes: `2` for usage errors, `1` for runtime errors (unknown key,
  already revoked, store failures).

### Managed keys over HTTP (`/api/auth/*`)

The same keys can be managed over HTTP with an `admin` token. All four routes
need `admin`, are never cacheable (`Cache-Control: no-store`), and answer
`501 Not Implemented` when the storage backend has no key store (plugin
stores); use the `CONSOLE_*_TOKENS` variables there. Env-defined tokens are not
keys: they are not listed and cannot be revoked here.

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/auth/keys` | Create a key; the token is returned once |
| `GET` | `/api/auth/keys` | List keys (no tokens, no digests) |
| `DELETE` | `/api/auth/keys/{id}` | Revoke a key |
| `GET` | `/api/auth/audit` | List key audit events, newest first |

**Create.** Body `{"name": "ci-bot", "scope": "read", "expires_at": "2027-01-01T00:00:00Z"}`;
`expires_at` is optional, RFC3339 and must be in the future. The body is
capped at 4 KiB (`413` beyond) and unknown fields are rejected.

```bash
curl -sX POST localhost:8080/api/auth/keys -H "Authorization: Bearer $CONSOLE_TOKEN" \
  -d '{"name":"ci-bot","scope":"read"}'
# → 201 {"id":"…","name":"ci-bot","scope":"read","status":"active",
#        "created_at":"…","expires_at":null,"last_used_at":null,"revoked_at":null,
#        "key":"csk_…"}
```

`key` is the plaintext token. It appears only in this response; only its
SHA-256 digest is stored. Statuses: `400` (missing or oversized name, invalid
scope, past or malformed `expires_at`, bad JSON), `403` (the requested scope is
above the caller's own: a caller can never mint a key more powerful than
itself), `409` (name already in use), `413`.

**List.** `200` with an array of `{id, name, scope, status, created_at,
expires_at, last_used_at, revoked_at}`; `status` is `active`, `expired` or
`revoked`.

**Revoke.** `204` on success, `404` for an unknown id, `409` if already
revoked. Revocation applies on the next request. Revoking the key that
authenticated the request (including a dashboard session signed in with it) is
refused with `409` to avoid locking yourself out; add `?force=true` (exact
value) to override.

**Audit.** `GET /api/auth/audit?since=<RFC3339>&limit=<n>` returns `[{id, at,
principal, action, target, request_id}]`. `limit` defaults to 100 and is at most
1000 (`400` otherwise). When `CONSOLE_AUDIT_RETENTION_DAYS` is above 0, `since`
is floored at now minus the retention window, so events awaiting the daily
prune are never returned. `request_id` is the `X-Request-Id` of the request that
caused the event (empty for CLI actions). The acting principal is the caller's
audit name; with auth disabled (loopback) there is no principal, so events are
recorded as `anonymous` and the scope-ceiling check is skipped.

Browser sessions work too, but unsafe requests (`POST`, `DELETE`) with a session
cookie must carry the session's CSRF token (`X-CSRF-Token`); bearer-token
clients need none.

The `curl` examples in the rest of this page omit the header for brevity; add
`-H "Authorization: Bearer $CONSOLE_TOKEN"` when auth is enabled.

See [runtime hardening](security/runtime-hardening.md) for rotation, the
reverse-proxy and login-throttle caveats, and the `CONSOLE_AUTH` setting.

## Errors

Errors return a JSON body `{"error": "..."}` with one of these status codes:

| Status | When |
|---|---|
| `400 Bad Request` | malformed JSON body or invalid parameter |
| `401 Unauthorized` | missing or invalid token (`WWW-Authenticate: Bearer`) |
| `403 Forbidden` | valid token, insufficient scope (or a failed CSRF check) |
| `429 Too Many Requests` | login throttled (`Retry-After`), `/login` only |
| `404 Not Found` | the addressed key doesn't exist |
| `409 Conflict` | creating a key that already exists |
| `413 Content Too Large` | key-create body above 4 KiB |
| `500 Internal Server Error` | unexpected backend failure |
| `501 Not Implemented` | `/api/auth/*` on a backend with no key store |

```jsonc
{ "error": "flag \"unknown-key\" not found" }
```

## Health

### `GET /healthz`

Unauthenticated liveness probe: `200` with body `ok` when the process is
serving. It does not touch the store or run checks. Use it for Docker,
Kubernetes and load-balancer probes and uptime monitors.

```bash
curl localhost:8080/healthz
```

### `GET /api/health`

Requires the `read` scope. Returns the aggregate snapshot — the latest check per component rolled up into
one overall state (worst-wins; `unknown` never masks a real `down`).

```bash
curl -H "Authorization: Bearer $CONSOLE_TOKEN" localhost:8080/api/health
```

```jsonc
{
  "state": 3,                       // 0 unknown · 1 operational · 2 degraded · 3 down
  "components": [
    { "component": "api",    "state": 1, "message": "status 200",        "latency": 41200000, "checked_at": "2026-06-15T12:00:00Z" },
    { "component": "worker", "state": 3, "message": "request failed: ...", "checked_at": "2026-06-15T12:00:00Z" }
  ],
  "checked_at": "2026-06-15T12:00:01Z"
}
```

## Flags

### `GET /api/flags`

```bash
curl localhost:8080/api/flags
```

Returns an array of flags.

### `POST /api/flags`

Create a flag. The body is a `core.Flag`. Returns `201` with the created flag,
or `409` if the key already exists.

```bash
curl -X POST localhost:8080/api/flags \
  -H 'Content-Type: application/json' \
  -d '{
        "key": "new-dashboard",
        "description": "New dashboard UI",
        "enabled": true,
        "scope": "beta",
        "rollout": 50
      }'
```

### `GET /api/flags/{key}`

```bash
curl localhost:8080/api/flags/new-dashboard
```

```jsonc
{
  "key": "new-dashboard",
  "description": "New dashboard UI",
  "enabled": true,
  "scope": "beta",
  "rollout": 50,
  "created_at": "2026-06-15T11:00:00Z",
  "updated_at": "2026-06-15T11:00:00Z"
}
```

### `PUT /api/flags/{key}`

Replace a flag. Body is a `core.Flag`. `404` if the key doesn't exist.

```bash
curl -X PUT localhost:8080/api/flags/new-dashboard \
  -H 'Content-Type: application/json' \
  -d '{"key":"new-dashboard","enabled":true,"scope":"beta","rollout":100}'
```

### `DELETE /api/flags/{key}`

```bash
curl -X DELETE localhost:8080/api/flags/new-dashboard
```

### `POST /api/flags/{key}/evaluate`

Evaluate a flag for a subject. The body is a `core.Subject`; the response is a
`core.Evaluation`.

Request (`core.Subject`):

```jsonc
{ "key": "user-123", "attributes": { "audience": "beta" } }
```

Response (`core.Evaluation`):

```jsonc
{
  "flag_key": "new-dashboard",
  "enabled": true,
  "variant": "on",                 // "on"/"off" for boolean; variant key otherwise
  "value": "",                     // present for multivariate variants
  "reason": "rollout_included"     // flag_disabled | out_of_scope | rollout_excluded | rollout_included | variant
}
```

```bash
curl -X POST localhost:8080/api/flags/new-dashboard/evaluate \
  -H 'Content-Type: application/json' \
  -d '{"key":"user-123","attributes":{"audience":"beta"}}'
# → {"flag_key":"new-dashboard","enabled":true,"variant":"on","reason":"rollout_included"}
```

See [flags](flags.md) for the evaluation pipeline and reason codes.

## Components

### `GET /api/components`

```bash
curl localhost:8080/api/components
```

### `POST /api/components`

Create a component. Body is a `core.Component`. `201` on success, `409` on a
duplicate key.

```bash
curl -X POST localhost:8080/api/components \
  -H 'Content-Type: application/json' \
  -d '{
        "key": "api",
        "name": "Public API",
        "provider": "http",
        "config": { "url": "https://example.com/health", "expect_status": "200", "timeout": "2s" }
      }'
```

### `GET /api/components/{key}` · `PUT /api/components/{key}` · `DELETE /api/components/{key}`

Get, replace, or delete a component by key (`404` if absent).

Component `config` secrets are write-only. A config key is secret if its
lowercase name contains `token`, `secret`, `key`, `password`, `credential`,
`auth`, `webhook`, `dsn`, `passwd`, `pass`, `pwd`, `bearer`, `cookie`,
`signature`, `jwt`, `private` or `connection_string`; every read (API, dashboard, MCP, logs) renders such values
as the literal `"[set]"`. Other keys are returned verbatim. On `PUT`, a secret
key whose value is `"[set]"`, or that is omitted, keeps its stored value, so a
GET-then-PUT round trip does not destroy a secret; any other value replaces it, and an empty string `""` clears the secret.
Redaction is by key name only: credentials embedded in the `url` value (userinfo
or query parameters) are **not** redacted, so keep them out of `url` and use a
secret-named key instead.
Status checks still use the real stored values.

### `POST /api/components/{key}/check`

Run a check now and return the resulting `core.Check`.

```bash
curl -X POST localhost:8080/api/components/api/check
```

```jsonc
{
  "component": "api",
  "state": 1,
  "message": "status 200",
  "latency": 41200000,
  "checked_at": "2026-06-15T12:00:00Z"
}
```

See [status](status.md) for provider config and the state mapping.

## Integrating from app code

There is no native SDK yet — call the API directly. A minimal `fetch` wrapper
for flag evaluation:

```js
async function evaluate(flagKey, subject, base = "http://localhost:8080") {
  const res = await fetch(`${base}/api/flags/${flagKey}/evaluate`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${process.env.CONSOLE_EVAL_TOKEN}`, // an eval-scope token
    },
    body: JSON.stringify(subject), // { key, attributes }
  });
  if (!res.ok) throw new Error(`evaluate ${flagKey}: ${res.status}`);
  return res.json(); // { flag_key, enabled, variant, value, reason }
}

const { enabled, variant } = await evaluate("cta-copy", {
  key: "user-123",
  attributes: { audience: "beta" },
});
```

The equivalent curl is shown under each endpoint above. A native client SDK is
on the roadmap.
