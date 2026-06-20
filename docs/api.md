# HTTP API reference

Console exposes a JSON HTTP API over the flag and status engines. The dashboard
is just a client of this same API, and it is how your applications integrate
today — there is **no native client SDK yet** (it's on the roadmap; integrate via
HTTP for now).

- [Conventions](#conventions)
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

- **No API auth yet.** There is no token or session check — anyone who can reach
  the listen address can read and write. Keep it bound to loopback
  (`CONSOLE_ADDR` default `127.0.0.1:8080`) and front it with your own auth if you
  expose it.
- **Request bodies are capped at 1 MiB.** An over-limit body is rejected rather
  than buffered.
- The server sets security headers on every response: `X-Content-Type-Options:
  nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, and a
  restrictive Content-Security-Policy.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/health` | Aggregate health snapshot |
| `GET` | `/api/flags` | List flags |
| `POST` | `/api/flags` | Create a flag |
| `GET` | `/api/flags/{key}` | Get a flag |
| `PUT` | `/api/flags/{key}` | Update a flag |
| `DELETE` | `/api/flags/{key}` | Delete a flag |
| `POST` | `/api/flags/{key}/evaluate` | Evaluate a flag for a subject |
| `GET` | `/api/components` | List components |
| `POST` | `/api/components` | Create a component |
| `GET` | `/api/components/{key}` | Get a component |
| `PUT` | `/api/components/{key}` | Update a component |
| `DELETE` | `/api/components/{key}` | Delete a component |
| `POST` | `/api/components/{key}/check` | Run a check now |

## Errors

Errors return a JSON body `{"error": "..."}` with one of these status codes:

| Status | When |
|---|---|
| `400 Bad Request` | malformed JSON body |
| `404 Not Found` | the addressed key doesn't exist |
| `409 Conflict` | creating a key that already exists |
| `500 Internal Server Error` | unexpected backend failure |

```jsonc
{ "error": "flag \"unknown-key\" not found" }
```

## Health

### `GET /api/health`

Returns the aggregate snapshot — the latest check per component rolled up into
one overall state (worst-wins; `unknown` never masks a real `down`).

```bash
curl localhost:8080/api/health
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
    headers: { "Content-Type": "application/json" },
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
