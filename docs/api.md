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

The `curl` examples in the rest of this page omit the header for brevity; add
`-H "Authorization: Bearer $CONSOLE_TOKEN"` when auth is enabled.

See [runtime hardening](security/runtime-hardening.md) for rotation, the
reverse-proxy and login-throttle caveats, and the `CONSOLE_AUTH` setting.

## Errors

Errors return a JSON body `{"error": "..."}` with one of these status codes:

| Status | When |
|---|---|
| `400 Bad Request` | malformed JSON body |
| `401 Unauthorized` | missing or invalid token (`WWW-Authenticate: Bearer`) |
| `403 Forbidden` | valid token, insufficient scope (or a failed CSRF check) |
| `429 Too Many Requests` | login throttled (`Retry-After`), `/login` only |
| `404 Not Found` | the addressed key doesn't exist |
| `409 Conflict` | creating a key that already exists |
| `500 Internal Server Error` | unexpected backend failure |

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
