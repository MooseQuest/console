# Security Policy

Console is an early (`0.x`) project from MooseQuest LLC. We take security
seriously and appreciate reports that help keep Console and its users safe.

## Hardening & deployment

> **Console currently has no built-in API authentication or authorization.**
> Any HTTP client that can reach the server can read and modify flags and
> components, and can read component `config` values that may contain API
> tokens.
>
> Until authentication ships, you **must** run Console bound to loopback
> (`127.0.0.1`, the default) and access it through an SSH tunnel, or place it
> behind an authenticating reverse proxy. Never expose Console directly on a
> network interface without an authenticating layer in front of it.
>
> See [docs/security/runtime-hardening.md](docs/security/runtime-hardening.md)
> for the full explanation, current mitigations, and the remediation roadmap.

### Security Standard Operating Procedures (SOPs)

Three living documents cover Console's security posture:

- [**Runtime hardening**](docs/security/runtime-hardening.md) — API auth gap
  (the #1 priority), secrets handling, server-side request forgery (SSRF) and
  egress controls, CSRF, security headers, and DoS limits. Start here.
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
