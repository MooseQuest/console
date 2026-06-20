# Supply-chain security SOP

Console is a self-hostable Go control plane. Its supply chain has three
surfaces: the Go module graph (host and plugins), the GitHub Actions CI
pipeline, and plugin binaries that operators place on disk and point the host
at. This document covers policy, tooling, and the routine checks that keep all
three surfaces clean.

Related documents:
- [Plugin trust & isolation](plugin-trust.md) — plugin binary distribution as a
  supply-chain input; operator and contributor checklists.
- [Runtime hardening](runtime-hardening.md) — server, secrets, network, and DoS
  controls.

---

## Dependency policy

### Stdlib-first, minimal dependencies

Console's coding conventions (see [CONTRIBUTING.md](../../CONTRIBUTING.md))
require reaching for the standard library before adding a third-party package.
Every new direct dependency must clear four questions before merging:

1. **Necessity.** Does the standard library already provide this, or close
   enough?
2. **License.** Is the license compatible with AGPL-3.0? Check
   [pkg.go.dev](https://pkg.go.dev) or the repo's `LICENSE` file. Permissive
   (MIT, Apache-2.0, BSD) is fine. GPL/AGPL dependencies need explicit review
   because they may impose additional obligations on the combined work.
3. **Maintenance.** Is the module actively maintained? Look for recent commits,
   open critical issues, and whether the module has a single maintainer with no
   succession plan.
4. **Transitive footprint.** Run `go mod graph | grep <module>` to see what
   else comes in. A small module that drags in a large, poorly-maintained graph
   is not small.

Document the justification in the PR description. Reviewers should reject
additions that don't clear all four gates.

### `go.sum` integrity and `-mod=readonly`

`go.sum` records the cryptographic hash of every module zip that the build has
fetched. It must be committed and reviewed as part of any dependency change.

- **Never commit a `go.sum` change without a corresponding `go.mod` change** —
  a `go.sum`-only diff is a red flag that a transitive dependency was silently
  upgraded or injected.
- CI builds with a committed `go.sum` (Go's default `-mod=readonly`), so the
  build fails fast if `go.sum` is out of sync with `go.mod` rather than
  silently downloading and adding modules.
- Do not use `go get -u ./...` for bulk upgrades; update one dependency at a
  time so the diff is reviewable.

---

## Vulnerability scanning

### `govulncheck`

[govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) analyzes
the call graph to distinguish vulnerabilities that are actually reachable from
those that are imported but never called. A vulnerability in an imported
package is not necessarily exploitable; `govulncheck`'s call-graph analysis
reduces false-positive noise.

**Running locally:**

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

Run this before opening a PR that adds or upgrades a dependency, and before
any release.

**In CI:** `govulncheck ./...` runs on every push and pull request. A finding
does not auto-block the build today; the CI step reports findings so they can
be triaged immediately. Findings are escalated to blocking before each release.

**Triage guidance:**

| `govulncheck` output | Meaning | Action |
|---|---|---|
| "Vulnerability #VVV in YOUR code" | The affected symbol is reachable from Console's own call graph. | High priority — patch or mitigate before the next release. |
| "Vulnerability #VVV in a test binary" | Reachable only in test code, not in the production binary. | Medium priority — patch in the next planned maintenance window. |
| "Vulnerability #VVV is imported but not called" | The module is present in the graph but the affected function is never called. | Low priority — note it, upgrade opportunistically, confirm no code path changed on next review. |
| Not mentioned by `govulncheck` | Module appears in `go.sum` but the vulnerable symbol is neither imported nor indirectly reachable. | Informational — track via Dependabot; no immediate action required. |

When a critical vulnerability is "imported but not called," still investigate
whether future code changes could make it reachable before the next planned
upgrade.

---

## Dependabot

Dependabot is enabled for two ecosystems:

- **`gomod`** — opens PRs for direct and indirect Go module upgrades.
- **`github-actions`** — opens PRs to update action references (see below).

Review and merge Dependabot PRs promptly. A dependency PR that sits open for
weeks is a signal that the upgrade is blocked by something — find out what.
Security-tagged Dependabot PRs (those labeled `security`) should be reviewed
within 48 hours.

---

## Pinning GitHub Actions to commit SHAs

Actions referenced in `.github/workflows/` are pinned to full commit SHAs
(`uses: actions/checkout@abc1234...`) rather than floating tags (`@v4`). This
prevents a compromised or overwritten tag from injecting code into CI without
a visible diff.

The policy:

- All `uses:` lines in workflow files must reference a full 40-character SHA.
- The human-readable tag is kept as a comment on the same line
  (`# v4.2.2`) so it is clear which tagged release the SHA corresponds to.
- Dependabot's `github-actions` config keeps these SHAs current.
- Do not add new workflow steps with floating tags; Dependabot will not pin
  them automatically on first add.

---

## Build and release integrity

### cgo-free, reproducible builds

Console is cgo-free (see [architecture.md](../architecture.md)). Builds are
deterministic given the same Go toolchain version: no cgo, no build-time
randomness, no network calls during compilation. This means release binaries
can be independently reproduced by any operator who has the same Go version
and the source at the tagged commit.

```bash
CGO_ENABLED=0 go build -trimpath -o console ./cmd/console
```

The `-trimpath` flag strips file-system paths from the binary, improving
reproducibility and removing local path disclosure.

### Signed tags

Release tags are signed with the maintainer's GPG key. Verify before building
from a tag:

```bash
git tag -v v0.3.0
```

### Release artifact checksums

The release workflow produces a `SHA256SUMS.txt` file alongside each release
archive. Operators should verify the checksum of any binary they download:

```bash
sha256sum -c SHA256SUMS.txt
```

### Future: SLSA provenance

Producing [SLSA](https://slsa.dev) provenance attestations (build provenance
at level 2 or 3) is on the roadmap. This would allow operators to
cryptographically verify that a released binary was produced by the official
CI pipeline from the tagged source, not from a developer's laptop or a
compromised build environment.

---

## Plugin distribution as a supply-chain input

Plugin binaries (`console-plugin-*`) are separate executables that the host
launches with elevated trust — they inherit the host's environment, including
all credentials. An operator who runs a third-party plugin is trusting that
binary as much as the host itself.

The plugin distribution channel is therefore a supply-chain surface. See
[plugin-trust.md](plugin-trust.md) for the full threat model, the "golden
rule" for running plugin binaries, and the planned checksum verification
(SecureConfig) and signing/allowlist campaign.

---

## htmx CDN risk and the vendor + SRI plan

The dashboard currently loads htmx from a CDN
(`https://unpkg.com/htmx.org/...`). This has two risks:

1. **Integrity.** If the CDN serves a tampered file (supply-chain compromise
   of the CDN or the package), every browser loading the dashboard would
   execute attacker-controlled JavaScript.
2. **Availability.** The dashboard is unavailable if the CDN is unreachable —
   a concern for air-gapped or network-restricted deployments.

**Current state:** 🔜 Planned. The mitigation is to vendor htmx into
`internal/web/static/` (already embedded via `embed`) and serve it from the
Console binary itself, with a Subresource Integrity (SRI) hash in the
`<script>` tag as a belt-and-suspenders check. The server's CSP is tightened
at the same time — dropping the CDN exception (see
[runtime-hardening.md §5](runtime-hardening.md#5-security-headers)).

Until this lands, operators who require strict integrity controls should
audit the htmx version in use.

---

## Routine checklists

### Per-PR checklist (author)

- [ ] Does this PR add or upgrade a dependency? If yes: answered the four
  dependency-policy questions in the PR description, and reviewed the `go.sum`
  diff.
- [ ] `govulncheck ./...` is clean locally (or findings are documented and
  triaged in the PR).
- [ ] Any new GitHub Actions workflow step uses a pinned SHA, not a floating
  tag.

### Per-release checklist

- [ ] `govulncheck ./...` — no "Vulnerability in YOUR code" findings open.
- [ ] All Dependabot security PRs are merged or have a documented exception.
- [ ] Release tag is signed. `SHA256SUMS.txt` file is generated and attached.
- [ ] Plugin binaries in the release archive have checksums in `SHA256SUMS.txt`.
- [ ] Check `go list -m all` for any modules added since the last release that
  have not gone through the dependency-policy review.

### Monthly maintenance

- [ ] Review open Dependabot PRs; merge or close with explanation.
- [ ] Run `govulncheck ./...` against `main`; triage any new findings.
- [ ] Review the Go toolchain version in use; upgrade if a security patch has
  been issued (see [go.dev/security](https://go.dev/security/)).
- [ ] Check the htmx version currently loaded from CDN against the upstream
  release page for any reported security issues.
