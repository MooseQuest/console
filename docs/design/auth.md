# API authentication and authorization — design

Status: proposed · Issue: #61 · Epic: #62 · Owner: Kristerpher Henderson

## 1. Context

Console has no built-in authentication. Any client that can reach the listener
can read and write flags and components and read component `config` values
(API tokens) verbatim. This is the documented #1 risk
([SECURITY.md](../../SECURITY.md),
[runtime-hardening.md](../security/runtime-hardening.md) section 1); the only
mitigation today is the loopback default bind (v0.2.1) plus operator-supplied
proxies. This document designs the built-in replacement.

Facts about the current code that shape the design:

- Routes live in one `http.ServeMux` (`internal/server/server.go`), wrapped by
  `logging(recoverPanic(securityHeaders(mux)))`. A single auth middleware slots
  in cleanly.
- `GET /api/health` returns the full component health snapshot (names and
  states), not a bare liveness signal. It is **not** safe to exempt as-is.
- The CLI (`flag`, `status`, `onboard`) opens the store in-process via
  `app.New`; it does not use HTTP. Only `console mcp -addr` is an HTTP client.
- `store.Store` is a plugin seam (SQLite built in, Postgres over gRPC). Anything
  added to it requires a plugin protocol change.
- The default Docker image binds `0.0.0.0:8080` (`docs/docker.md`).
- Go 1.26 stdlib provides `http.CrossOriginProtection`, `crypto/subtle`, and
  `crypto/sha256`; no new dependency is needed for any of this.

## 2. Invariants (binding)

1. **No replayable credential at rest.** The server persists or holds only
   SHA-256 digests of API tokens, never the plaintext. Plaintext is shown once
   at creation (phase 2) or lives only in the operator's env/secret store
   (phase 1). Tokens are never written to logs, error bodies, metrics, or the
   database in raw form.
2. **Secrets live in env or files, rotatable without code change.** Every token
   variable has a `_FILE` variant; rotation is overlap-based (section 4.1).
3. **Default-deny on exposure.** A non-loopback bind with no authentication is
   refused at startup unless the operator explicitly opts out.
4. **Audit trail for every state change.** Each mutating request logs principal
   name, scope, method, route pattern, and request ID (APM #57). Phase 2 adds a
   persisted audit record for key create/revoke.
5. **Stdlib-first, cgo-free.** No new dependencies. Auth is an interface seam
   (`auth.Authenticator`) wired only in `internal/app/app.go`.
6. **`internal/core` stays dependency-free**; scope and principal types live in
   the new `internal/auth` package.
7. **Flag evaluation stays deterministic.** Auth gates the endpoint; it never
   participates in evaluation.

## 3. Decisions (summary)

| Question (#61) | Decision |
|---|---|
| Credential model | Opaque bearer API keys, high entropy, stored/compared as SHA-256 digests. Static env-defined keys first (phase 1); DB-managed keys later (phase 2). |
| Scopes | Four hierarchical scopes: `eval` < `read` < `write` < `admin`. |
| Dashboard auth | Login form exchanging a token for a server-side session cookie. Not HTTP Basic. |
| CSRF | Per-session synchronizer token plus `http.CrossOriginProtection`; bearer-authenticated requests are exempt. |
| `/healthz` | New minimal unauthenticated liveness endpoint. `/api/health` requires `read`. |
| `/metrics` | Requires `read` when auth is enabled. Unauthenticated only while auth is off (loopback). Separate metrics listener must be loopback or authenticated. |
| Startup policy | Refuse non-loopback bind without auth; staged rollout. |
| Clients | `console mcp -addr` gains token via env/file; CLI needs nothing. |
| Config secrets | Write-only: redacted on every read path. |
| SSO/OIDC, per-user accounts | Deferred; the `Authenticator` seam leaves room. |

### 3.1 Credential model: API keys over a single admin token

A single shared admin token is simplest but cannot express least privilege (a CI
job that only evaluates flags would hold full write access) and cannot be rotated
without an outage. Per-key scopes fix both at small cost. The token itself is a
random 256-bit value with a recognisable prefix (`csk_`, so secret scanners can
match it), base64url encoded. Because it is high-entropy, a fast digest (SHA-256)
is sufficient; a password KDF would add latency for no security gain.
Comparison is constant-time over digests (`subtle.ConstantTimeCompare`).

Rejected alternatives: JWTs (needs signing-key management and revocation story
for no benefit in a single-process control plane); mTLS (Console does not
terminate TLS by design; operators can still do this at the proxy); HTTP Basic
for the API (sends a reusable secret in a form clients cannot scope).

### 3.2 Scopes and route mapping

| Scope | Allows | Routes |
|---|---|---|
| `eval` | Evaluate a flag | `POST /api/flags/{key}/evaluate` |
| `read` | All reads, dashboard view, metrics | `GET /api/*`, `GET /`, `/flags`, `/status`, `/metrics` |
| `write` | Mutations and check triggers | `POST/PUT/DELETE /api/*`, `POST /flags/{key}/toggle`, `POST /status/{key}/check` |
| `admin` | Key management, future settings | `/api/auth/*` (phase 2) |

Scopes are hierarchical (`admin` implies all). `POST .../check` is `write`
because it triggers outbound requests and persists results. The route-to-scope
map is a single table in `internal/server` keyed by mux pattern, with a test
asserting that every registered route has an entry (a new route without a scope
fails the build, preventing silent exposure).

### 3.3 Exemptions

Unauthenticated always: `GET /healthz`, `GET /static/`, `GET /login`,
`POST /login` (rate-limited). Everything else needs a principal.

`/healthz` is added as a new endpoint returning `200 ok` with no body detail.
The issue text mentions `/healthz`, but today only `/api/health` exists and it
discloses component inventory, so it is moved behind `read`. Docker and
Kubernetes probes should use `/healthz`.

### 3.4 `/metrics` decision (APM epic #54)

`/metrics` is treated as `read`-scoped. Rationale: it is cheap for Prometheus to
present a bearer token (`authorization:` in scrape config), metrics reveal
traffic shape and route usage, and exempting it would create a permanent hole in
the default-deny rule. Concretely:

- Auth enabled: `/metrics` on the main listener requires `read`.
- Auth disabled (loopback only): unauthenticated, as in apm.md section 4.5.
- `CONSOLE_METRICS_ADDR` (separate listener): allowed unauthenticated only if
  loopback; a non-loopback metrics address requires the same token check.
  There is no "public metrics" escape hatch.
- pprof keeps its own loopback guard (`CONSOLE_PPROF_ALLOW_REMOTE`) and is
  never served on the main listener.

APM card #47 must therefore register `/metrics` through the same scope table.

## 4. Design

### 4.1 Configuration and rotation

| Env | Meaning |
|---|---|
| `CONSOLE_ADMIN_TOKENS` | Comma-separated tokens granting `admin` |
| `CONSOLE_WRITE_TOKENS` | Tokens granting `write` |
| `CONSOLE_READ_TOKENS` | Tokens granting `read` |
| `CONSOLE_EVAL_TOKENS` | Tokens granting `eval` |
| `*_FILE` variants | Read the list from a file (one token per line, mode 0600 recommended) |
| `CONSOLE_AUTH` | `auto` (default) or `disabled` (explicit opt-out) |

Multiple tokens per scope make rotation zero-downtime: add the new token, move
clients, remove the old token, restart (SIGHUP reload is a nice-to-have, not
required). Token display names for audit are `scope-N` plus the first 6 hex
characters of the digest, never any part of the token.

`console auth token` prints a freshly generated token for the operator to place
in their secret store; it writes nothing.

Phase 2 adds DB-managed keys (named, scoped, optional expiry, `last_used_at`,
revocable) behind a separate optional interface, not `store.Store`:

```go
// KeyStore is optional; the SQLite store implements it. Plugin stores that
// do not implement it fall back to env-defined tokens only.
type KeyStore interface {
    CreateKey(ctx context.Context, k Key) error      // stores digest only
    LookupKey(ctx context.Context, digest [32]byte) (Key, error)
    ListKeys(ctx context.Context) ([]Key, error)     // never returns digests as secrets
    RevokeKey(ctx context.Context, id string) error
}
```

Keeping it off `store.Store` avoids a plugin protocol change for Postgres; the
Postgres plugin can implement it later via an additive RPC.

### 4.2 Seam

```go
package auth

type Scope int // Eval < Read < Write < Admin

type Principal struct {
    Name  string // audit label, never the token
    Scope Scope
}

// Authenticator turns a request into a Principal or ErrUnauthenticated.
type Authenticator interface {
    Authenticate(r *http.Request) (Principal, error)
}
```

`Static` (env tokens), `Chain` (static then KeyStore), and later `OIDC` are
implementations. The server middleware extracts `Authorization: Bearer <token>`
or a valid session cookie, resolves the required scope from the pattern table,
and responds `401` (with `WWW-Authenticate: Bearer`) or `403` (insufficient
scope). Error bodies never echo the credential. The principal is stored in the
request context for the audit log.

### 4.3 Request flow

```mermaid
sequenceDiagram
  participant C as Client
  participant M as auth middleware
  participant A as Authenticator
  participant H as Handler
  C->>M: request (Bearer token or session cookie)
  M->>M: exempt route? (healthz, static, login)
  M->>A: Authenticate(r)
  A-->>M: Principal or ErrUnauthenticated
  alt no principal
    M-->>C: 401 WWW-Authenticate: Bearer (or 303 to /login for HTML)
  else scope < required(pattern)
    M-->>C: 403
  else cookie auth and unsafe method
    M->>M: CSRF check (CrossOriginProtection + token)
    M->>H: serve with Principal in context
  end
  H-->>C: response; mutation logged with principal + request ID
```

Middleware order becomes `logging(recoverPanic(securityHeaders(auth(mux))))`;
once APM lands, the request-ID and metrics middleware stay outermost and auth
sits inside them so failed attempts are counted
(`console_auth_failures_total{reason}`, no token-derived labels).

### 4.4 Dashboard sessions and CSRF

`POST /login` takes a token, authenticates it, and sets a session cookie.
Sessions are server-side and in-memory: a random 256-bit ID mapped to
{principal, token digest, csrf token, expiry}. Restart logs everyone out, which
is acceptable for a single-process tool and avoids needing a signing secret.
On every request the session's token digest is re-checked against the current
token set, so removing a token revokes its sessions immediately.

Cookie: `HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` when the request is TLS
or `X-Forwarded-Proto: https` is set by a trusted proxy
(`CONSOLE_COOKIE_SECURE=auto|always`). Idle timeout 12h, absolute 7d. `POST
/logout` destroys the session.

CSRF: sessions carry a token delivered via a template variable, sent as
`X-CSRF-Token` by htmx (`hx-headers` on `<body>`) and as a hidden field on plain
forms; `http.CrossOriginProtection` provides Origin/Sec-Fetch-Site defence in
depth. Bearer-authenticated API calls carry no ambient credential and are
exempt. Login failures are throttled per remote address with exponential
backoff in memory. The CSP in `securityHeaders` already permits the login form
(no inline script).

HTTP Basic was rejected for the dashboard: no logout, credential resent on every
request, and no place to attach a CSRF token.

### 4.5 Startup policy

At `serve` start: if `CONSOLE_AUTH != disabled`, tokens are configured, auth is
enabled. If the bind address is not loopback (empty host such as `:8080` counts
as non-loopback; reuse the logic of `isLoopback` in `cmd/console/qr.go`, moved
to a shared package) and auth is not enabled, **exit with a clear error**
naming the two fixes (set a token, or `CONSOLE_AUTH=disabled` acknowledging a
fronting proxy). `CONSOLE_AUTH=disabled` on a non-loopback bind logs a warning
at every startup. Loopback with no tokens runs open exactly as today.

### 4.6 Clients

- **CLI:** no change. It uses the store in-process; its trust boundary is
  filesystem/DB access, which is documented.
- **`console mcp -addr`:** `httpBackend` adds `Authorization: Bearer`. Token is
  read from `CONSOLE_TOKEN`, or `-token-file`; there is deliberately **no**
  `-token` flag (it would leak through `ps` and shell history). The client
  refuses to send a token over plain `http://` to a non-loopback host unless
  `-insecure-token` is passed. MCP `-write` tools need a `write` token; the
  server's scope check is the authority, and MCP read-only mode should be
  paired with a `read` token.
- **SDK/apps:** use an `eval` token.
- Token storage by operators: env from a secret manager or a 0600 file.

### 4.7 Component `config` secrets (write-only)

Auth reduces who can read secrets but does not remove the exposure; combine with
the write-only plan in runtime-hardening section 2. Decision: redact on **every**
output path (JSON API, dashboard, MCP output, logs). A config key is secret if
its lowercase name contains `token`, `secret`, `key`, `password`, `credential`,
`auth` or `webhook`; redacted values render as the literal `"[set]"`. Fail-safe
over precise. On `PUT`, a value equal to `"[set]"` (or an omitted key) preserves
the stored value, so a read-modify-write round trip cannot destroy a secret.
Even `admin` cannot read stored values via the API. A later refinement is a
provider-declared secret-field list. At-rest encryption remains a separate item
(`CONSOLE_SECRET_KEY`).

## 5. Data model and migration

Phase 1: no schema change; no migration; rollback is removing the env vars.
Phase 2 (SQLite): one additive table, created idempotently on open:

```sql
CREATE TABLE IF NOT EXISTS api_keys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  digest BLOB NOT NULL UNIQUE,   -- sha256 of the token
  scope TEXT NOT NULL,           -- eval|read|write|admin
  created_at TIMESTAMP NOT NULL,
  expires_at TIMESTAMP,
  last_used_at TIMESTAMP,
  revoked_at TIMESTAMP
);
CREATE TABLE IF NOT EXISTS audit_events (
  id TEXT PRIMARY KEY, at TIMESTAMP NOT NULL, principal TEXT NOT NULL,
  action TEXT NOT NULL, target TEXT NOT NULL
);
```

Rollback: older binaries ignore the extra tables; dropping them loses only keys.

## 6. Rollout

1. **Dark (release N):** auth code ships, active only when tokens are
   configured. Non-loopback without auth logs a loud warning. `/healthz` added.
2. **Default-deny (release N+1):** startup refusal enabled (section 4.5). The
   Docker image binds `0.0.0.0`, so docs and compose file must pass
   `CONSOLE_ADMIN_TOKENS` (or the explicit opt-out) before this ships.
3. **Phase 2:** managed keys and audit events.
4. SECURITY.md and runtime-hardening.md are updated in the release that makes
   auth available, not after.

Since the project is `0.x`, the breaking startup change is acceptable but must
be called out in release notes.

## 7. Security and privacy checklist

- **PII:** none collected. Token names are operator-chosen labels; remote
  addresses appear only in operator-controlled logs.
- **Retention/deletion:** sessions in memory only; `api_keys` rows deleted on
  revoke+purge; audit events retained until the operator prunes (default
  unbounded, `console auth audit prune` proposed in phase 2). No data-subject
  records exist to erase.
- **Audit:** principal name, scope, route pattern, status, request ID. No
  tokens, no request bodies, no config values.
- **Replayable credentials:** none stored; digests only. A stolen database
  yields no usable tokens. Env/file tokens are plaintext in the operator's
  secret store by nature, which is why `_FILE` and 0600 are recommended.
- **Breach response:** rotate by adding a new token and removing the old one;
  removal revokes live sessions at once. Report via SECURITY.md.
- **Kill-switch:** removing a token revokes it; `CONSOLE_AUTH=disabled`
  exists only as the deliberate opt-out, never as an emergency default.
- **Timing/DoS:** constant-time digest comparison; failed-auth throttling;
  body size limits already in place.

## 8. Open questions (operator decision)

1. Confirm `/api/health` moves behind `read` and a new `/healthz` is the probe
   endpoint (breaking for anyone polling `/api/health` unauthenticated).
2. Confirm staged default-deny (release N warn, N+1 refuse) versus refusing
   immediately.
3. Docker image default: require a token (recommended) or keep an explicit
   opt-out in the quickstart.
4. Is `eval` as a separate scope worth the extra tier now? (Recommended: yes.)
5. Deferred: OIDC/SSO, per-user accounts, SIGHUP token reload, per-key rate
   limits, mTLS. None block phase 1.

## 9. Proposed implementation cards

See the PR description; cards are filed after review.
