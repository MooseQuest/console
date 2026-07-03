# Documentation Remediation Plan — 2026-07-02

## Summary
- **Corpus reviewed:** 20 prose docs — `README.md`, `CONTRIBUTING.md`, `CLA.md`,
  `SECURITY.md`; `docs/{api,architecture,development,flags,getting-started,mcp,notifications,onboarding,plugins,plugins-architecture,status}.md`;
  `docs/business/license-analysis.md`; `docs/security/{plugin-trust,runtime-hardening,supply-chain}.md`.
- **Skipped (with reason):** `CHANGELOG.md` (release notes), `CLAUDE.md` (agent
  notes, gitignored), `dist/SHA256SUMS.txt` (generated),
  `docs/librarian/remediation-{2026-06-19,2026-06-26}.md` (prior deliverables of
  this skill).
- **Weights (defaults):** duplicate 0.40 · concise 0.35 · sound 0.25. Flag when
  composite < 3.5 **or** any criterion ≤ 2.
- **Flagged by score:** 1 — `README.md` (3.40). No doc scored ≤ 2 on any single
  criterion, but **11 docs carry functional (Phase 3) errors despite scoring ≥ 3.5**;
  those are listed below and are the required work.
- **Tally:** Duplicates/merges 0 required (plugins.md/plugins-architecture.md
  verdict = keep both, re-confirmed) · Reductions 2 optional · Grammar/MLA 3 ·
  Functional ~21 · Completeness 2 (a dangling `CONTRIBUTORS.md` ref in two files).
- **Ground truth verified against source this pass:** repo at **v0.5.0**;
  `ls -d cmd/console-plugin-* | wc -l` = **12**; `ls internal/notify` =
  `discord,email,pagerduty,slack,webhook` (5 sinks); `go.mod` = `go 1.25.11` with
  **8 direct deps, all pure-Go/cgo-free** (`modernc.org/sqlite`, `hashicorp/go-plugin`,
  `google.golang.org/grpc`+`protobuf`, `jackc/pgx/v5`, `mdp/qrterminal/v3`,
  `modelcontextprotocol/go-sdk`); `console mcp` subcommand present
  (`cmd/console/main.go` `case "mcp"`, `internal/mcp/`).

> **This plan supersedes `remediation-2026-06-26.md`, which was never implemented.**
> Its still-open items were re-verified against current source and are folded in
> below (plugin counts, notifications sinks, getting-started version links —
> now further stale at v0.5.0, `os.Environ` claim, "a integrity" typo, supply-chain
> action pins + tag, license version, RSALv2, CSRF expansion). This pass adds three
> new themes from the v0.5.0 release: **(1)** the `mcp` subcommand is missing from
> CLI lists; **(2)** the "only dependency is sqlite" / "no dependencies" framing is
> now false in several docs; **(3)** a dangling `CONTRIBUTORS.md` reference.
>
> **Confirmed already-current (no change):** `docs/mcp.md` (new, accurate),
> `api.md`, `flags.md`, `status.md`, `onboarding.md`, `development.md`, `plugins.md`,
> `plugins-architecture.md`. `architecture.md` already documents the MCP consumer
> surface correctly — only its plugin count and notify sub-list are stale.

## Priority order
1. **`README.md`** — stale catalog ("Ten"→12), notify list missing discord/pagerduty, CLI list missing `mcp`, "no dependencies" framing (composite 3.40; self-contradicts its own L139).
2. **`docs/notifications.md`** — "Three notifiers ship" (should be five); missing Discord + PagerDuty in two places (sound 3).
3. **`docs/getting-started.md`** — three hardcoded `v0.3.0` download strings (release is v0.5.0); only outright-wrong install steps.
4. **`docs/architecture.md`** — "10" plugins (→12) and notify sub-list missing discord/pagerduty.
5. **`CONTRIBUTING.md`** — inline "v0.3.0", "10" plugins, CLI list missing `mcp`, dangling `CONTRIBUTORS.md`.
6. **`docs/security/runtime-hardening.md`** — SSRF/log-scrub omit discord+pagerduty; MCP write surface unacknowledged; "a integrity" typo.
7. **`docs/business/license-analysis.md`** — "only dependency is sqlite" false (3 spots); "repo is at v0.3.0".
8. **`docs/security/supply-chain.md`** — stale `# v4` action-pin examples (real = v6), `git tag -v v0.3.0`, deps-now-multiple note.
9. **`docs/security/plugin-trust.md`** — `os.Environ()` mechanism misstated.
10. **`CLA.md`** — dangling `CONTRIBUTORS.md`. **`SECURITY.md`** — expand CSRF on first use.

---

## Per-document findings

### README.md — composite 3.40 (dup 3 / concise 4 / sound 3)
- **Functional (do first):**
  - **L254:** "it runs with zero plugins. **Ten ship today**: **store** — `postgres`; …" → **Twelve ship today**. Verify: `ls -d cmd/console-plugin-* | wc -l` → 12.
  - **L255:** "**notify** — `slack`, `webhook`, `email`;" → "**notify** — `slack`, `discord`, `webhook`, `email`, `pagerduty`;" (matches L139's "Five ship today").
  - **L287:** "`cmd/console-plugin-*/    10 out-of-process plugin binaries …`" → **12**.
  - **L286 (Architecture block) and the CLI usage block (~L144-150):** both list `serve, flag, status, onboard, qr, version` and omit **`mcp`**. Add a `console mcp   Serve Console over the Model Context Protocol (for AI agents)` line to the CLI usage block, and add `mcp` to the L286 parenthetical `(serve, flag, status, onboard, qr, mcp, version)`.
  - **L30:** "**One binary, no dependencies.**" is now false framing (8 direct deps). Reframe to the real invariant, e.g. "**One binary, cgo-free.**" — keep the body sentence about pure-Go SQLite and the static binary; only the "no dependencies" claim is wrong.
  - **L285+ (Architecture package block):** add an `internal/mcp/` line now that MCP is a shipped surface (mirror architecture.md's wording: "MCP server (a consumer surface)").
  - **Verified correct, do NOT change:** version v0.5.0, Go 1.25.11+, the L139 "Five ship today" notify prose, the L198-208 MCP section (consistent with `docs/mcp.md`), `support@moosequest.net`.
- **Redundancy (optional reduction):** the plugin catalog is restated four times (Notifications §, Plugins § L254-256, Configuration tables, Architecture block) and the copies drift. Canonical home = the Configuration tables + Plugins §; let the Architecture block reference rather than re-enumerate counts.
- **Style note:** preserve the confident second-person marketing voice; fix facts only.

### docs/notifications.md — composite 4.10 (dup 4 / concise 5 / sound 3)
- **Redundancy:** catalog/config overlaps `plugins-architecture.md` (the canonical catalog); notifications.md's unique value is the event model + emission/dispatcher semantics. Keep that; defer catalog detail.
- **Functional:**
  - **L5-6:** "**Three notifiers ship** as out-of-process plugins — Slack, webhook, and email —" → "**Five notifiers ship** … — Slack, Discord, webhook, email, and PagerDuty —". Verify: `ls internal/notify`.
  - **L53-56 (blockquote):** "**Two more notifiers ship** as plugins: `console-plugin-webhook` … and `console-plugin-email` (SMTP)." → "**Four more notifiers ship** as plugins: `console-plugin-discord`, `console-plugin-webhook`, `console-plugin-email` (SMTP), and `console-plugin-pagerduty`."
  - **L86 (optional, low priority):** "The shipped plugins do exactly this: `console-plugin-webhook` …; `console-plugin-email` …" — consider adding discord/pagerduty for parity.
- **Style note:** preserve the "doesn't just observe" lede, the event-type table, the Severity color mapping, and the emission diagram.

### docs/getting-started.md — composite 4.75 (dup 5 / concise 5 / sound 4)
- **Functional (do first):**
  - **L23:** `.../download/v0.3.0/console_v0.3.0_darwin_arm64.tar.gz` → **v0.5.0** (path segment + filename).
  - **L24:** `.../download/v0.3.0/SHA256SUMS.txt` → **v0.5.0**.
  - **L26:** `tar xzf console_v0.3.0_darwin_arm64.tar.gz && cd console_v0.3.0_darwin_arm64` → **v0.5.0** (tarball name **and** extracted dir). Verify: `dist/` holds `console_v0.5.0_*`; latest `git tag` = v0.5.0. Leave the `releases/latest` line (version-agnostic).
  - **Verified correct:** "Go 1.25.11+", `CONSOLE_ADDR` default, `make plugins`, AI-mode plugin names.
- **Completeness (optional):** the "Where to go next" list (L165-171) omits the new `console mcp`; add a one-line pointer to `docs/mcp.md` (do not duplicate mcp.md).
- **Style note:** preserve the "arrow → outcome" comment style and the install→onboard narration.

### docs/architecture.md — composite 4.10 (dup 4 / concise 5 / sound 3)
- **Functional:**
  - **L31:** "the **10** out-of-process plugin executables" → **12**.
  - **L44:** "`internal/notify/{slack,webhook,email}/   plugin notifier sinks`" → `internal/notify/{slack,discord,webhook,email,pagerduty}/`. Verify: `ls internal/notify`.
  - **Verified correct:** the MCP consumer surface is already documented (L30 cmd list includes `mcp`; L53-54 describe `internal/mcp/`). No change there.
- **Completeness (optional):** the doc never explicitly says MCP is *not* a seam; its placement outside the seam table already implies it. A one-clause clarification is optional.
- **Style note:** preserve the rationale-forward voice, em-dash asides, and the one-way-dependency framing.

### CONTRIBUTING.md — composite 3.95 (dup 4 / concise 5 / sound 3)
- **Functional:**
  - **L3:** "Thanks for your interest in Console (**v0.3.0**)." → **v0.5.0** (or drop the inline version to avoid drift).
  - **L61:** "`cmd/console-plugin-*/  10 out-of-process plugin binaries`" → **12**.
  - **L60:** "CLI entrypoint (serve, flag, status, onboard, qr, version)" → add **`mcp`**.
  - **Verified correct:** "Go 1.25.11+" (L35), "four extension seams" (L102), the cgo-free framing (L90).
- **Completeness (dangling ref):** **L24-25** instruct contributors to add their name to `CONTRIBUTORS.md`, which **does not exist**. Either create a `CONTRIBUTORS.md` (with a short header) or reword to "create/append to `CONTRIBUTORS.md`". (Same ref in CLA.md §8 — fix together.)
- **Style note:** preserve the crisp imperative maintainer voice; "(PR)" already defined on first use.

### docs/security/runtime-hardening.md — composite 4.50 (dup 5 / concise 5 / sound 3)
- **Functional:**
  - **§3 SSRF, L113:** "Notify plugins POST to webhook URLs and Slack incoming webhook URLs." → also enumerate **discord** (webhook URL) and **pagerduty** (Events API v2 HTTP POST). Verify: `internal/notify/{discord,pagerduty}/`.
  - **§2 log-scrubbing, L73-74:** "(from Slack and webhook plugins …)" → add discord (secret webhook URL) and pagerduty (routing key), or generalize to "URL-/credential-bearing notify sinks."
  - **MCP surface (new) — add a short note near §1:** `console mcp` serves over **stdio** by default (trust boundary = the local user launching the subprocess); `-addr host:port` targets the HTTP API and **inherits the no-auth gap**, so it belongs behind loopback/an authenticating proxy; `-write` enables mutating tools and is **off by default**. Source: `cmd/console/mcp_cmd.go`, `internal/mcp/`.
  - **Verified correct, do NOT change:** the header table (L165-168) and CSP string (L175) match `internal/server/server.go` exactly.
- **Grammar/MLA:** **L187:** "will include **a** `integrity=` SRI hash" → "**an** `integrity=` SRI hash". (This typo exists **only here**; supply-chain.md L202 "a Subresource Integrity (SRI) hash" is correct — do not touch it.)
- **Style note:** preserve the "#1 priority / what you must do until X lands" framing and the status summary table.

### docs/business/license-analysis.md — composite 4.15 (dup 5 / concise 4 / sound 3)
- **Functional:**
  - "**The only external dependency is `modernc.org/sqlite`**" is now false (8 direct deps). Fix the "sole dependency" claims:
    - **L386:** reframe to list current direct deps (sqlite, go-plugin, grpc, protobuf, pgx, qrterminal, modelcontextprotocol/go-sdk) for the license-compat discussion.
    - **L319:** "our existing **single** external dependency (modernc.org/sqlite)" → "our external dependencies" (plural).
    - **L346:** "existing dependency (modernc.org/sqlite under MIT)" → note multiple deps now warrant the compat check.
    - L24 describes the SQLite *driver*, not a sole-dependency claim — leave unless tightening.
  - **L358:** "The repo is at **v0.3.0** and still early (0.x)" → **v0.5.0** (still 0.x).
  - **Do NOT change** the `2026-06-15` decision-date stamps, nor the already-correct MIT→past-tense framing (L27).
- **Completeness/Grammar:** **RSALv2** (L168, L300, L420-421) never expanded on first use → "Redis Source Available License v2 (RSALv2)" at L168.
- **Reduction (optional):** the "not legal advice" disclaimer appears ~3× (top banner + close) — deliberate for a legal doc; reduce only if the maintainer wants it tighter.
- **Publication check:** confirm `docs/business/` is not served by GitHub Pages (Pages builds from `docs/index.html`). Clean of `kris@` either way.
- **Style note:** preserve the emphatic "research-only / NOT legal advice" voice and the source-per-section discipline.

### docs/security/supply-chain.md — composite 4.50 (dup 5 / concise 5 / sound 3)
- **Functional:**
  - **L114 / L121:** illustrative Action-pin examples use v4 ("floating tags (`@v4`)", "(`# v4.2.2`)"). Real workflow pins are **v6** (`.github/workflows/ci.yml`: `actions/checkout@…# v6.0.3`, `actions/setup-go@…# v6.4.0`). Update the examples to a v6 pin so they match the repo.
  - **L151:** "`git tag -v v0.3.0`" → a current tag (**v0.5.0**).
  - **Dependency policy:** add a one-line note that direct deps are now several (all pure-Go/cgo-free) — sqlite, go-plugin, grpc, protobuf, pgx, qrterminal, and the newest, `modelcontextprotocol/go-sdk` (MCP) — so the reader doesn't assume the sqlite-only era holds. Govulncheck scope covers them all.
  - **Verified correct:** the htmx-still-on-CDN status; L202 SRI wording.
- **Style note:** preserve the SOP/checklist voice (per-PR / per-release / monthly) and the four dependency-gate questions.

### docs/security/plugin-trust.md — composite 4.10 (dup 4 / concise 5 / sound 3)
- **Functional:**
  - **L122:** "Today the host passes **`os.Environ()`** to each plugin subprocess." — **mechanism is false.** `internal/plugin/host.go:40` uses `exec.Command(path)` with `Cmd.Env` left nil, so the subprocess **implicitly inherits the host environment** (Go uses the parent env when `Cmd.Env` is nil); the host never calls `os.Environ()`. Verify: `grep -rn 'os.Environ\|Cmd.Env' internal/plugin/` (none). Reword to describe the nil-`Cmd.Env`/implicit-inheritance mechanism. The *effect* the doc describes (all host env reaches the plugin — L36, L68 table) is correct; keep it.
  - **Verified correct:** AutoMTLS (`host.go:42`), ProtocolVersion=1 (`plugin.go:28`), absolute-path launch, and the planned SecureConfig/minimal-env items.
- **Reduction (optional):** the "Attack surface summary" table restates the "What it does NOT do" prose — trim one.
- **Style note:** preserve the "golden rule" voice and ✅/🔜 status glyphs.

### CLA.md — composite 5.00 (dup 5 / concise 5 / sound 5)
- **Completeness (dangling ref):** **L99 (§8)** tells contributors to add their name to `CONTRIBUTORS.md`, which does not exist. Fix alongside CONTRIBUTING.md L24-25 (create the file or reword to "create/append").
- **Verified correct:** `support@moosequest.net` (no `kris@`); the `license-analysis.md` §8 link resolves.
- **Style note:** preserve the formal legal-draft register and the DRAFT banner.

### SECURITY.md — composite 4.75 (dup 5 / concise 5 / sound 4)
- **Grammar/MLA (completeness):** **L~27** uses "CSRF" without expansion while "SSRF" is spelled out on the same line. Expand to "cross-site request forgery (CSRF)" on first use.
- **Verified correct:** `support@moosequest.net`; loopback default + no-auth statements; all `docs/security/*` links resolve.
- **Style note:** preserve the calm security-advisory tone.

### Docs confirmed clean (no required edits)
- **`docs/mcp.md`** (5.00, NEW) — tool table, `-write`/`-addr` behavior, resources/prompt, and go-sdk framing all match `internal/mcp/server.go` + `cmd/console/mcp_cmd.go`.
- **`docs/api.md`** (5.00), **`docs/flags.md`** (5.00), **`docs/status.md`** (5.00), **`docs/onboarding.md`** (5.00), **`docs/development.md`** (5.00) — all source-verified current. Optional only: a one-line `docs/mcp.md` pointer in onboarding.md/api.md (do not duplicate).
- **`docs/plugins.md`** (4.60) & **`docs/plugins-architecture.md`** (4.60) — **verdict re-confirmed: keep both** (authoring guide vs. canonical mechanics/catalog); no new divergence; plugins-architecture.md's catalog is current ("Twelve", discord+pagerduty).

---

## Execution notes for the fixer (LLM/agent)
- Apply edits **in place**; preserve each doc's voice/formatting (see per-doc **Style note**). Mechanics and facts only — no new sections beyond those specified (the runtime-hardening MCP note and an optional `CONTRIBUTORS.md` file are the only additions sanctioned here).
- **Propagate from current docs:** `plugins-architecture.md` (catalog/count) and `development.md` (Go version) are correct at v0.5.0 — use them as the reference.
- **Verify each functional item before applying:**
  - Plugin count: `ls -d cmd/console-plugin-* | wc -l` → 12.
  - Notify sinks: `ls internal/notify` → discord, email, pagerduty, slack, webhook.
  - Release tag → v0.5.0 (getting-started, CONTRIBUTING, supply-chain, license-analysis).
  - CLI set: `cmd/console/main.go` → serve, flag, status, onboard, qr, mcp, version.
  - Deps: `go mod edit -json` (8 direct, all pure-Go) for the dependency-framing fixes.
  - `os.Environ`: `grep -rn 'os.Environ\|Cmd.Env' internal/plugin/` (none) + read `internal/plugin/host.go`.
  - Action pins: `.github/workflows/ci.yml` (checkout@…v6, setup-go@…v6).
- **Do NOT touch:** the CSP string + header table in `runtime-hardening.md`; the `2026-06-15` decision-date stamps in `license-analysis.md`; supply-chain.md L202 SRI wording; the `releases/latest` link in `getting-started.md`.
- The `CONTRIBUTORS.md` dangling reference spans two files (CONTRIBUTING.md, CLA.md) — resolve consistently (create the file, or reword both).
- Treat "optional" reductions/pointers as maintainer's discretion; the **functional**, **grammar**, and **dangling-ref** items are the required set.
- After edits, re-run a `docs/**` link-check and re-render `docs/index.html` if the site is rebuilt.
