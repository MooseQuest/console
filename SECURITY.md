# Security Policy

Console is an early (`0.x`) project from MooseQuest LLC. We take security
seriously and appreciate reports that help keep Console and its users safe.

## Hardening & deployment

Console has **built-in authentication**: scoped bearer tokens (`eval`, `read`,
`write`, `admin`) for the API and a token login for the dashboard.

- Generate a token with `console auth token` and configure it with
  `CONSOLE_ADMIN_TOKENS` (or `CONSOLE_ADMIN_TOKENS_FILE`; also `_WRITE_`,
  `_READ_`, `_EVAL_`). Tokens are static; rotate by changing the configuration
  and restarting.
- `console serve` **refuses to start on a non-loopback address** (including
  `:8080` and `0.0.0.0`, and the container default) unless a token is
  configured. With no token it runs open on loopback only, so keep that to local
  use.
- `/healthz` is the only unauthenticated API endpoint (plus `/login` and static
  assets). `GET /api/health` and `/metrics` need a `read` token.
- Component `config` secrets are write-only: secret-named keys read back as
  `"[set]"`.

Known limitations: secrets embedded in a component's `url` value are not
redacted; `localhost` is trusted by name in the loopback check; behind a reverse
proxy the login throttle sees one client. Terminate TLS in front of Console.

See [docs/security/runtime-hardening.md](docs/security/runtime-hardening.md)
for the full explanation, scopes, rotation, and remaining roadmap.

### Security Standard Operating Procedures (SOPs)

Three living documents cover Console's security posture:

- [**Runtime hardening**](docs/security/runtime-hardening.md) — built-in
  authentication, scopes and rotation, secrets handling, server-side request forgery (SSRF) and
  egress controls, cross-site request forgery (CSRF), security headers, and DoS
  limits. Start here.
- [**Plugin trust & isolation**](docs/security/plugin-trust.md) — the plugin
  subprocess threat model, AutoMTLS, planned checksum verification
  (SecureConfig), and minimal-env isolation. Operator and contributor
  checklists.
- [**Supply-chain security SOP**](docs/security/supply-chain.md) — dependency
  policy, `govulncheck`, Dependabot, pinned GitHub Actions SHAs, build/release
  integrity, and the htmx-from-CDN risk.

## Reporting a vulnerability

Please report security vulnerabilities by email to **support@moosequest.net**.

- **Do not open a public GitHub issue** for a vulnerability — that discloses it
  before a fix is available. Use email so we can triage and patch privately.
- Include enough detail to reproduce: affected version or commit, the impact,
  and steps or a proof of concept if you have one.
- We'll acknowledge your report, keep you posted as we investigate, and credit
  you in the release notes once a fix ships (unless you'd prefer otherwise).

## Supported versions

While Console is on `0.x`, only the **latest release** receives security fixes.
Please upgrade to the most recent release before reporting, and expect fixes to
land in a new release rather than as backports to older `0.x` versions.
