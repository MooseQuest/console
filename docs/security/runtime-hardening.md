# Console runtime security

This document covers the security properties of a running Console instance:
the HTTP API and dashboard, secrets handling, network egress, CSRF, and
resource limits. It states what is done today and what is planned, in
priority order. Authentication is covered first (§1).

Related documents:
- [Plugin trust & isolation](plugin-trust.md) — the plugin subprocess threat
  model, AutoMTLS, planned checksum verification and minimal env.
- [Supply-chain security SOP](supply-chain.md) — dependency policy,
  `govulncheck`, Dependabot, pinned Actions.

---

## 1. Authentication

Console has built-in authentication. When at least one API token is configured,
every route except the exempt ones below requires a credential with a
sufficient scope. Without any token, Console runs open **only on a loopback
bind**; it refuses to start on any other address (see
[Startup refusal](#startup-refusal)).

### Tokens and scopes

Tokens are static bearer secrets configured per scope through the environment:

| Variable | Scope |
|---|---|
| `CONSOLE_ADMIN_TOKENS` | `admin` |
| `CONSOLE_WRITE_TOKENS` | `write` |
| `CONSOLE_READ_TOKENS` | `read` |
| `CONSOLE_EVAL_TOKENS` | `eval` |

Each accepts a comma-separated list. Every variable also has a `_FILE` form
(for example `CONSOLE_ADMIN_TOKENS_FILE`): one token per line, blank lines and
lines starting with `#` ignored. Setting both `NAME` and `NAME_FILE` is a
startup error. Prefer the `_FILE` form (Docker/Kubernetes secrets) so tokens
stay out of process listings and `docker inspect`.

Scopes are hierarchical: `admin` includes `write`, which includes `read`, which
includes `eval`. Generate a token with `console auth token` (a `csk_` prefix plus
256 bits of randomness; nothing is stored). Clients send
`Authorization: Bearer <token>`. Tokens are compared in constant time and are
never logged; the audit log identifies a caller by scope and a short digest
label only.

| Scope | Allows |
|---|---|
| `eval` | `POST /api/flags/{key}/evaluate` only |
| `read` | everything read-only: `GET /api/*`, the dashboard, `/metrics` |
| `write` | create, update, delete, toggle flags and components; trigger checks |
| `admin` | everything `write` does (reserved for future admin operations) |

The authoritative route-to-scope table is `routeScopes` in
`internal/server/authz.go`; a test fails the build if a registered route has no
entry. Exempt (no credential): `GET /healthz`, `GET /static/`, and the
`/login` and `/logout` routes. A missing or invalid credential gets `401` with
`WWW-Authenticate: Bearer`; a valid token with too low a scope gets `403`.

### Rotation

Tokens are read at startup, so rotating means editing the configuration and
restarting. To rotate without downtime for clients, list the old and new token
together (`CONSOLE_READ_TOKENS=old,new`), restart, move clients to the new
token, then remove the old one and restart again. Removing a token also ends any
dashboard session that logged in with it: sessions are re-checked against the
configured tokens on every request.

### Startup refusal

`console serve` exits non-zero before listening if `CONSOLE_ADDR` is not
loopback (this includes `:8080`, `0.0.0.0`, and `[::]`) and no token is
configured. The same rule applies to a non-loopback `CONSOLE_METRICS_ADDR` (unless
`CONSOLE_METRICS=off`). The Docker image binds `0.0.0.0:8080`, so a container
needs a token to start.

Loopback is judged from the configured address string: an IP in `127.0.0.0/8`
or `::1`, or the literal host name `localhost`. The name `localhost` is trusted
as written and is not resolved, so a hosts file or resolver that maps it
elsewhere would defeat the check. Use `127.0.0.1` if that matters to you.

### Escape hatch: `CONSOLE_AUTH=disabled`

If Console sits behind your own authenticating proxy (OAuth2 proxy, mTLS,
Tailscale ACLs) and you do not want a second credential, set
`CONSOLE_AUTH=disabled`. This turns authentication off even if tokens are set,
permits non-loopback binds, and logs a warning on every startup. Only do this
when nothing but the authenticating proxy can reach the port; Console then has
no access control of its own. It is deliberately absent from every quickstart
and compose file. Any other value of `CONSOLE_AUTH` besides `auto` (the
default) is a startup error.

### Dashboard sessions, CSRF and login throttling

The dashboard signs in with a token at `/login`. The server keeps an in-memory
session (lost on restart, so users sign in again) and sets a `console_session`
cookie: `HttpOnly`, `SameSite=Lax`, `Path=/`. A session expires after 12 hours
idle or 7 days absolute.

- **Secure cookie.** `CONSOLE_COOKIE_SECURE=auto` (default) sets `Secure` when
  the request arrived over TLS or carries `X-Forwarded-Proto: https`;
  `always` forces it. Set `always` when TLS terminates at a proxy, and make the
  proxy set or overwrite `X-Forwarded-Proto` itself. This header affects
  **only** the `Secure` attribute of the cookie; Console trusts no other
  forwarded header.
- **CSRF.** Cookie-authenticated state-changing requests need the session CSRF
  token (`X-CSRF-Token` header for htmx, `csrf_token` form field) and pass
  Go's cross-origin check (`Origin` / `Sec-Fetch-Site`). Requests with an
  `Authorization` header never use the cookie and are exempt from CSRF.
- **Login throttling.** A client gets 3 free failed logins; the 4th
  failure starts a 2s lockout that doubles on each further failure (2s, 4s, 8s,
  ...) up to 15 minutes. Locked-out clients receive `429` with `Retry-After`. State is in memory and bounded.

**Reverse-proxy caveat.** The throttle keys on the TCP peer address
(`RemoteAddr`). `X-Forwarded-For` is deliberately ignored because any client can
forge it. Behind a reverse proxy, every client therefore shares the proxy's
address as one key: a handful of failed logins from anyone can lock out
everybody, and an attacker is not slowed per source. Apply rate limiting and
brute-force protection at the proxy as well, and use long random tokens (the
generated ones are 256-bit, so guessing is not practical regardless).

### What auth does not cover

- **Secrets embedded in a component's `url`** (userinfo such as
  `https://user:pass@host`, or a query string like `?token=...`) are **not**
  redacted. Redaction is by config key name (see §2), and `url` is not a secret
  key name. Keep credentials out of `url`; put them in a secret-named key such
  as `api_token`.
- **Tokens are static.** There is no per-token expiry, revocation list, or
  managed key store yet; rotate by changing configuration.
- **No TLS in Console.** Terminate TLS at a proxy or load balancer; bearer
  tokens and the session cookie are otherwise sent in clear text.

### The MCP server surface

The `console mcp` subcommand is a second entry point to the same data. By
default it serves over **stdio**, so its trust boundary is simply the local user
who launched the subprocess. Two flags change the exposure:

- `-write` registers the mutating tools (create/toggle/delete flags and
  components). It is **off by default** — without it the server is read- and
  evaluation-only.
- `-addr host:port` points the server at a running `console serve` over its JSON
  API instead of the local store. Against an authenticated server it needs a
  token: set `CONSOLE_TOKEN` or pass `-token-file` (`read` scope, or `write` with
  `-write`). The client refuses to send a token over plain `http://` to a
  non-loopback host. See [../mcp.md](../mcp.md#authenticating-to-a-server).

---

## 2. Secrets handling

### Problem

Component `config` maps hold API tokens in plaintext — for example, a
Cloudflare API token or Heroku API key stored directly in the component
record. These values are:

- Stored in plaintext in the SQLite or Postgres database.
- Previously returned verbatim by the API; that is fixed, see "Write-only
  config" below.

### Log scrubbing — ✅ Done (v0.2.1)

URL-bearing error messages (from URL-/credential-bearing notify sinks — Slack,
Discord, webhook, and PagerDuty — where the webhook URL or routing key is itself
a secret) previously logged the full URL. Credential redaction
is now applied before logging: webhook URLs are truncated to their scheme and
host, and the path (which contains the token) is replaced with `[redacted]`.

### Write-only config — ✅ Done

Component `config` values whose key name looks like a credential (it contains
`token`, `secret`, `key`, `password`, `credential`, `auth`, `webhook`, `dsn`,
and similar; the full list is in [../api.md](../api.md)) are returned as the
literal `"[set]"` on every read path: the API, the dashboard, MCP, and logs.
On `PUT`, sending `"[set]"` or omitting the key keeps the stored value, so a
GET-then-PUT round trip does not destroy a secret.

Known limitation: redaction is by **key name only**. A credential embedded in
the `url` value (userinfo like `https://user:pass@host`, or a query parameter
such as `?token=...`) is returned and logged as written. Keep credentials out
of `url` and use a secret-named key instead.

This does not help if the database itself is compromised; it reduces the
blast radius of API-level exposure.

### At-rest encryption — 🔜 Planned

Sensitive fields in the database will be encrypted at rest using a
key derived from an operator-supplied secret (`CONSOLE_SECRET_KEY` or
equivalent). Encryption happens at the store layer, transparently to the
engines. The migration path (encrypting existing plaintext values) will be
documented before this ships.

Until this lands, treat the Console database file (SQLite) or Postgres schema
with the same care you apply to any database containing credentials: restrict
filesystem permissions, encrypt the volume or disk, and audit database access.

---

## 3. SSRF and egress controls

### Problem

Console makes outbound HTTP calls on behalf of operator configuration:

- Status providers make HTTP requests to external services based on the
  component's `url` field (the built-in `http` provider) or to provider APIs
  (Cloudflare, Heroku, Sentry plugins).
- Notify plugins POST to external endpoints: Slack and Discord incoming/channel
  webhooks, arbitrary webhook URLs, and the PagerDuty Events API.

None of these outbound calls currently filter private IP ranges or follow
redirect policies. An operator (or an attacker who has write access to the
API) can configure a `url` of `http://169.254.169.254/latest/meta-data/`
(AWS instance metadata), `http://100.100.100.200/` (Alibaba/GCP metadata), or
any internal service, and Console will dutifully fetch it and surface the
response in check results or error messages.

### Planned: egress allowlist and private-range blocking — 🔜

The remediation is a custom `http.Client` used by all outbound calls that:

1. Resolves the target hostname and rejects requests to private IP ranges
   (RFC 1918: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`; loopback:
   `127.0.0.0/8`; link-local: `169.254.0.0/16`; IPv6 equivalents).
2. Does not follow redirects that would cross the private/public boundary.
3. Enforces a per-request timeout (independent of the Go HTTP default).

An optional allowlist (`CONSOLE_EGRESS_ALLOWLIST`) will let operators
explicitly permit specific internal hosts (e.g., an on-premise status
endpoint) while keeping the default-deny posture for unrecognised private
addresses.

Until this lands: operators who run Console with access to cloud metadata
endpoints or sensitive internal services should run Console in a network
namespace or VPC that denies or rate-limits access to those addresses at the
network layer.

---

## 4. CSRF protection — ✅ Done

Cookie-authenticated state-changing requests (dashboard POSTs) require a
per-session CSRF token, sent as the `X-CSRF-Token` header (htmx) or the
`csrf_token` form field, and pass Go's `Origin` / `Sec-Fetch-Site` cross-origin
check. Session cookies are `SameSite=Lax` and `HttpOnly`. Login and logout are
covered too. Bearer-token requests carry no ambient credential and are exempt.
Failures return `403` and increment `console_csrf_failures_total`. See §1.

---

## 5. Security headers

### ✅ Done (v0.2.1)

A security-headers middleware was added to the HTTP server. The following
headers are set on every response:

| Header | Value |
|---|---|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Referrer-Policy` | `no-referrer` |
| `Permissions-Policy` | `camera=(), microphone=(), geolocation=()` |
| `Content-Security-Policy` | see below |

`Content-Security-Policy` is already a restrictive CSP with a single CDN
exception for htmx:

```
default-src 'self'; script-src 'self' https://unpkg.com; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'
```

The only non-`'self'` source is `https://unpkg.com` for the CDN-loaded htmx
script; that exception will be removed when htmx is vendored (see below).

### htmx CDN and CSP — 🔜 Planned

The dashboard loads htmx from `https://unpkg.com`. Once htmx is vendored into
`internal/web/static/` (see [supply-chain.md](supply-chain.md)), the CSP will
be updated to disallow external script sources and inline scripts. The `<script>`
tag will include an `integrity=` SRI hash for belt-and-suspenders protection
against a tampered embedded asset.

---

## 6. DoS and resource limits

### ✅ Done (v0.2.1)

Several unbounded resource paths were addressed:

**HTTP server timeouts.** The HTTP server now enforces `ReadTimeout`,
`WriteTimeout`, and `IdleTimeout`. A slow or misbehaving client cannot hold
a connection open indefinitely.

**Request-body size limit.** `http.MaxBytesReader` is applied to all request
bodies. Oversized requests (e.g., a multi-megabyte JSON payload) are rejected
before being read into memory.

**LLM response bounding.** LLM plugin responses are read through an
`io.LimitReader`. A misbehaving or compromised LLM plugin cannot cause the
host to allocate unbounded memory reading a response.

**RunAll concurrency bound.** The `status.Engine.RunAll` path (which fans out
a check to every registered component) is bounded by a semaphore. Previously
unbounded goroutine fan-out could exhaust file descriptors or scheduler
resources on large component inventories.

### Remaining risk

These controls prevent the most obvious resource exhaustion paths. They do not
prevent a sustained request flood from a network-accessible endpoint. Operators
who expose Console on a non-loopback interface should enforce rate limiting at
the reverse proxy layer.

## 7. pprof profiling endpoint

Go's `net/http/pprof` exposes heap, goroutine, CPU-profile and command-line
data. It is **unauthenticated** and can reveal memory contents and internals,
so Console treats it as a debugging tool, not a service.

- **Off by default.** Nothing listens unless `CONSOLE_PPROF_ADDR` is set
  (e.g. `CONSOLE_PPROF_ADDR=127.0.0.1:6060`).
- **Separate listener and mux.** pprof is served on its own port from a
  dedicated mux. It is never registered on `http.DefaultServeMux` or the main
  API/dashboard mux, so enabling it does not change the main listener's surface
  (a test asserts `/debug/pprof/*` is 404 there).
- **Loopback guard.** Only `localhost` and loopback IP literals
  (`127.0.0.1:6060`, `[::1]:6060`, `localhost:6060`) are accepted. Wildcard
  and routable addresses (`:6060`, `0.0.0.0:6060`, `10.0.0.5:6060`) and
  hostnames other than `localhost` are refused.
- **Refusal is a startup error.** `console serve` exits non-zero with a message
  naming the address and the override, rather than silently disabling pprof.
- **Override.** `CONSOLE_PPROF_ALLOW_REMOTE=1` (also `true`, `yes`, `on`) disables the guard. Only use it
  behind a network control you trust (private network, firewall, authenticating
  proxy); there is no authentication on the endpoint itself.
- **Shutdown.** The listener is closed gracefully with the main server on
  SIGINT/SIGTERM.

Usage: `go tool pprof http://127.0.0.1:6060/debug/pprof/heap`. For remote
hosts prefer an SSH tunnel to the loopback address over the override.

---

## 8. The `/metrics` endpoint

`/metrics` serves Prometheus-format metrics (see [../observability.md](../observability.md)).

- **Unauthenticated while auth is off.** Like the rest of the API, it has no
  access control until built-in auth is enabled. It is served on the main
  listener, so it inherits that listener's bind address (loopback,
  `127.0.0.1:8080`, by default).
- **With auth enabled it requires the `read` scope**
  ([../design/auth.md](../design/auth.md) section 3.4). Configure Prometheus with
  an `authorization` bearer token; see the scrape config in the observability doc.
- **Content.** Metrics contain no flag keys, subject keys, secrets, or component
  `config` values. They do reveal route names, provider names, traffic volume,
  and the Console version, which is reconnaissance value for an attacker.
- **Behind a proxy, block it.** If a reverse proxy exposes Console, deny
  `/metrics` at the proxy (or allow only your Prometheus host).
- **Or use a separate address.** `CONSOLE_METRICS_ADDR=127.0.0.1:9091` moves
  metrics off the main listener. A non-loopback address makes `serve`
  refuse to start unless auth is enabled, and the separate listener enforces
  the same `read`-scope token check as the main listener. The startup check is
  skipped when `CONSOLE_METRICS=off`.
  The separate listener serves only `/metrics`; `/healthz` stays on the main
  listener, and `/metrics` is then not served there.
- **Disable.** `CONSOLE_METRICS=off` removes the endpoint entirely.

---

## Summary: current state and roadmap

| Issue | Severity | Status |
|---|---|---|
| No API/dashboard auth | Critical | ✅ Built-in scoped tokens + dashboard login; startup refuses non-loopback without auth (BREAKING, `0.x`) |
| Secrets in API responses | Critical | ✅ Write-only `config` secrets (secrets embedded in `url` are not redacted) |
| Secrets plaintext at rest | High | 🔜 At-rest encryption planned |
| SSRF via status providers and webhooks | High | 🔜 Private-range-blocking egress client planned |
| CSRF on dashboard POSTs | High | ✅ Session CSRF token + cross-origin check |
| Login brute force behind a reverse proxy | Medium | Throttle keys on the TCP peer, so proxied clients share one key; rate limit at the proxy |
| Secrets leaking into logs via URL error messages | Medium | ✅ Credential redaction done (v0.2.1) |
| Unbounded LLM reads / no server timeouts / RunAll fan-out | Medium | ✅ All three bounded (v0.2.1) |
| Default bind to all interfaces | High | ✅ Changed to loopback by default (v0.2.1) |
| `/metrics` exposure | Medium | Unauthenticated while auth is off (loopback default); requires `read` when auth is enabled (both listeners); block at proxy or use `CONSOLE_METRICS_ADDR` |
| `localhost` trusted by name | Low | Loopback check does not resolve `localhost`; use `127.0.0.1` |
| pprof exposure | Medium | ✅ Opt-in, separate loopback-only listener (override required for remote) |
| Security headers | Low | ✅ Restrictive headers + CSP middleware added (v0.2.1) |
| htmx from CDN without SRI | Low | 🔜 Vendor + SRI; drop the CDN CSP exception |
