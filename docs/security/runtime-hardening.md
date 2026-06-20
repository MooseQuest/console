# Console runtime security

This document covers the security properties of a running Console instance:
the HTTP API and dashboard, secrets handling, network egress, CSRF, and
resource limits. It states what is done today and what is planned, in
priority order.

Related documents:
- [Plugin trust & isolation](plugin-trust.md) — the plugin subprocess threat
  model, AutoMTLS, planned checksum verification and minimal env.
- [Supply-chain security SOP](supply-chain.md) — dependency policy,
  `govulncheck`, Dependabot, pinned Actions.

---

## 1. Authentication gap — the #1 priority

**Console has no built-in API authentication or authorization.** Every HTTP
endpoint — flag reads and writes, component reads and writes, check triggers,
the dashboard — is accessible to any HTTP client that can reach the server.
The component `config` map (which holds plaintext API tokens for status
providers) is returned verbatim by the API.

This is the most critical open risk in the current codebase.

### What we have done to reduce exposure (✅ v0.2.1)

The default bind address was changed from `0.0.0.0` to `127.0.0.1`
(loopback, the default for `CONSOLE_ADDR`: `127.0.0.1:8080`). A default
`./console serve` is no longer reachable from the network without explicit
configuration. Operators who need network access must set `CONSOLE_ADDR` to
the desired address and understand what they are exposing.

### What you must do until auth lands (operator requirement)

**Do not expose Console directly on a network interface without an
authenticating layer in front of it.** Keep it on `127.0.0.1` (the default)
and reach it locally or via an SSH tunnel, place it behind a reverse proxy
(nginx, Caddy, Traefik) that enforces authentication — HTTP Basic Auth with
TLS, OAuth2 via a sidecar, or mTLS client certificates — or run it inside a
private network segment (VPC, internal Kubernetes namespace, Tailscale) where
only trusted principals can reach the port.

If Console is bound to a non-loopback address with no authenticating proxy,
credentials stored in component `config` and all flag and component state are
readable and writable by anyone with network access to that port. There is no
other technical control that compensates for this today.

### Planned: API and dashboard authentication — 🔜

The roadmap item for built-in auth will add session-based authentication for
the dashboard and API key or token authentication for the JSON API. The design
is not yet started. Until it ships, the reverse-proxy pattern above is the
only supported hardening posture.

---

## 2. Secrets handling

### Problem

Component `config` maps hold API tokens in plaintext — for example, a
Cloudflare API token or Heroku API key stored directly in the component
record. These values are:

- Returned verbatim in API responses (e.g. `GET /api/components/{key}`).
- Stored in plaintext in the SQLite or Postgres database.
- Reachable to anyone who can call the API (which, without auth, is anyone
  who can reach the server — see §1).

### Log scrubbing — ✅ Done (v0.2.1)

URL-bearing error messages (from Slack and webhook plugins, where the webhook
URL is itself a secret) previously logged the full URL. Credential redaction
is now applied before logging: webhook URLs are truncated to their scheme and
host, and the path (which contains the token) is replaced with `[redacted]`.

### Write-only / redacted API responses — 🔜 Planned

The `config` map in `GET /api/components/{key}` and `GET /api/components`
responses will be changed to return a redacted representation for any key that
Console recognises as a credential field (`api_token`, `auth_token`,
`api_key`, etc.). Operators will be able to write a new value but not read
back the stored value through the API. Display in the dashboard will show
`[set]` / `[not set]` rather than the value.

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
- Notify plugins POST to webhook URLs and Slack incoming webhook URLs.

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

## 4. CSRF protection — 🔜 Planned (depends on auth)

The dashboard makes state-changing POST requests from a browser session, but
because Console has no session authentication yet (§1), CSRF is academic
today — the attack requires a session that does not exist. When auth lands,
CSRF becomes active. Full token protection ships with the auth work: a
per-session CSRF token generated at login, embedded in every form and htmx
request header (`X-CSRF-Token`), validated server-side on every state-changing
request, backed by `SameSite=Lax` session cookies.

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
| `Content-Security-Policy` | see below |

`Content-Security-Policy` is already a restrictive CSP with a single CDN
exception for htmx:

```
default-src 'self'; script-src 'self' https://unpkg.com; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'
```

The only non-`'self'` source is `https://unpkg.com` for the CDN-loaded htmx
script; that exception will be removed when htmx is vendored (see below).

> `Permissions-Policy` is not set by the middleware today — 🔜 Planned.

### htmx CDN and CSP — 🔜 Planned

The dashboard loads htmx from `https://unpkg.com`. Once htmx is vendored into
`internal/web/static/` (see [supply-chain.md](supply-chain.md)), the CSP will
be updated to disallow external script sources and inline scripts. The `<script>`
tag will include a `integrity=` SRI hash for belt-and-suspenders protection
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

---

## Summary: current state and roadmap

| Issue | Severity | Status |
|---|---|---|
| No API/dashboard auth; `config` returns plaintext tokens | Critical | 🔜 **Run on loopback or behind authenticating proxy until auth lands** |
| Secrets in API responses and plaintext at rest | Critical | 🔜 Write-only API + at-rest encryption planned |
| SSRF via status providers and webhooks | High | 🔜 Private-range-blocking egress client planned |
| CSRF on dashboard POSTs | High | 🔜 Planned with session auth |
| Secrets leaking into logs via URL error messages | Medium | ✅ Credential redaction done (v0.2.1) |
| Unbounded LLM reads / no server timeouts / RunAll fan-out | Medium | ✅ All three bounded (v0.2.1) |
| Default bind to all interfaces | High | ✅ Changed to loopback by default (v0.2.1) |
| Security headers | Low | ✅ Restrictive headers + CSP middleware added (v0.2.1) |
| htmx from CDN without SRI | Low | 🔜 Vendor + SRI; drop the CDN CSP exception |
