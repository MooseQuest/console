# Documentation Remediation Plan — 2026-06-26

## Summary
- **Corpus reviewed:** 19 prose docs — `README.md`, `CONTRIBUTING.md`, `CLA.md`,
  `SECURITY.md`; `docs/{api,architecture,development,flags,getting-started,notifications,onboarding,plugins,plugins-architecture,status}.md`;
  `docs/business/license-analysis.md`; `docs/security/{plugin-trust,runtime-hardening,supply-chain}.md`.
- **Skipped (with reason):** `CHANGELOG.md` (auto-release notes), `dist/SHA256SUMS.txt`
  (generated), `docs/librarian/remediation-2026-06-19.md` (prior deliverable of this
  skill), `CLAUDE.md` (agent operating instructions, not user-facing docs).
- **Weights used (defaults):** duplicate/redundant 0.40 · clear & concise 0.35 ·
  objectively-sound-yet-clear 0.25. Flag threshold: composite < 3.5 **or** any single
  criterion ≤ 2.
- **Flagged by score:** 3 — `README.md` (2.75), `notifications.md` (3.00), `CONTRIBUTING.md` (3.35).
- **Additional docs with functional fixes (score ≥ 3.5 but content is wrong):** 6 —
  `getting-started.md`, `architecture.md`, `runtime-hardening.md`, `supply-chain.md`,
  `plugin-trust.md`, `license-analysis.md`.
- **Tally:** Duplicates/merges 4 · Reductions 2 · Grammar/MLA 6 · Functional 13.
- **Ground truth this pass verified against:** repo at **v0.4.0**; **12** `console-plugin-*`
  binaries; notify seam ships **5** sinks (`slack, discord, webhook, email, pagerduty`);
  status providers `cloudflare, heroku, sentry` (+ built-in `http`); LLM `anthropic, openai, ollama`;
  store SQLite (built-in) + `postgres`; Go floor **1.25.11**; default bind `127.0.0.1:8080`, no built-in auth.

> **Root cause of most findings:** the v0.4.0 release added `console-plugin-discord` and
> `console-plugin-pagerduty`. `plugins-architecture.md` and `development.md` were updated
> with the new catalog/count; several *other* docs that independently enumerate the catalog,
> the plugin count, or the notify sinks were **not** propagated and are now one release behind.
> `plugins-architecture.md` (catalog) and `development.md` (Go version) are the current
> source-of-truth docs — propagate from them.

---

## Priority order
1. **`docs/notifications.md`** — states "Three notifiers ship"; missing Discord + PagerDuty entirely; a reader would never learn paging exists (composite 3.00, sound 2).
2. **`README.md`** — "Ten ship today" + notify list of 3 (L242-243) and "10 ... binaries" (L275) are stale; example list contradicts its own prose (composite 2.75).
3. **`docs/getting-started.md`** — 3 hardcoded `v0.3.0` binary-download strings; the only outright-wrong install instructions a user would copy/paste.
4. **`docs/architecture.md`** — stale "10" plugin count (L31) and notify subpackage list missing `discord,pagerduty` (L44).
5. **`CONTRIBUTING.md`** — inline "v0.3.0" (L3) and "10 ... plugin binaries" (L61).
6. **`docs/security/runtime-hardening.md`** — SSRF/egress (§3) and log-scrubbing (§2) enumerations omit the new HTTP/webhook-calling sinks (discord, pagerduty).
7. **`docs/security/supply-chain.md`** — stale `# v4` action-pin examples and `git tag -v v0.3.0`; "a integrity" typo.
8. **`docs/business/license-analysis.md`** — "repo is at v0.3.0" (L358); expand RSALv2 on first use.
9. **`docs/security/plugin-trust.md`** — unverified `os.Environ()` code claim (L121).
10. **`SECURITY.md`** — expand CSRF on first use (parity with SSRF). Minor.

---

## Per-document findings

### docs/notifications.md — composite 3.00 (dup 3 / concise 4 / sound 2)
- **Redundancy:** The per-sink catalog + config env vars + Slack webhook example overlap
  `docs/plugins-architecture.md` (the catalog source of truth). Duplication partner:
  `plugins-architecture.md`.
- **Functional (do first):**
  - Lede (≈L5-7): "**Three notifiers ship as out-of-process plugins — Slack, webhook, and email**"
    → **Five**: add **Discord** and **PagerDuty**. Verify: `ls internal/notify` → discord, email, pagerduty, slack, webhook.
  - Blockquote (≈L53-57): "**Two more notifiers ship as plugins:** `console-plugin-webhook` …
    and `console-plugin-email`" — stale; must also cover `console-plugin-discord`
    (`CONSOLE_DISCORD_WEBHOOK_URL`) and `console-plugin-pagerduty` (`CONSOLE_PAGERDUTY_ROUTING_KEY`).
  - PagerDuty's trigger/resolve semantics (down/degraded triggers, recovery resolves, correlated
    by per-component dedup key, flag changes skipped) currently live **only** in
    `plugins-architecture.md §notes`. Add a one-line mention here so the paging behavior is discoverable.
- **Proposed action (recommended):** Make `plugins-architecture.md` the canonical catalog/config
  home. Reshape `notifications.md` to own only what is unique — the **event model** (event-type
  table + `Severity` color mapping + "first check counts as a transition" rule) and the
  **emission/dispatcher mechanics** (best-effort, per-sink timeout, `SetEmitter`) — and replace its
  per-sink catalog with one line: "All five sinks and their config live in
  [the plugin architecture](plugins-architecture.md)." **Regardless of restructuring, correct the
  count to five immediately.**
- **Grammar/MLA:** none outstanding (the prior plan's broken TOC anchor `#the-slack-notifier-plugin`
  is already fixed).
- **Style note:** Preserve the "doesn't just observe" lede voice, the event-type table, the
  Severity color mapping, and the ASCII emission diagram.

### README.md — composite 2.75 (dup 3 / concise 3 / sound 2)
- **Functional (do first):**
  - L242: "**Ten ship today:**" → "**Twelve ship today:**". Verify: `ls -d cmd/console-plugin-* | wc -l` → 12.
  - L243: "**notify** — `slack`, `webhook`, `email`" → "**notify** — `slack`, `discord`, `webhook`,
    `email`, `pagerduty`". (This is the catalog one-liner; it must match L139's prose, which already
    correctly says "Five ship today" for notify.)
  - L275 (Architecture tree): "`cmd/console-plugin-*/    10 out-of-process plugin binaries (store/status/notify/llm)`"
    → **12**.
  - L257-258 (notify bash example): the comment (L257) lists "Slack, Discord, webhook, email, PagerDuty"
    but the `export CONSOLE_NOTIFY_PLUGINS=` on L258 only includes `slack,webhook,email`. Either add
    `console-plugin-discord` + `console-plugin-pagerduty` to the export (and their
    `CONSOLE_DISCORD_WEBHOOK_URL` / `CONSOLE_PAGERDUTY_ROUTING_KEY` env lines), or make the comment a
    deliberate subset ("# a subset — see docs/plugins-architecture.md for all five"). Prefer adding them
    for consistency with L139.
  - **Verified correct, do NOT change:** version v0.4.0 (L8/L26/L50/L60), Go 1.25.11+ (L63/L95), bind
    `127.0.0.1:8080`, `support@moosequest.net`, and the L139 "Five ship today" notify prose.
- **Redundancy (optional reduction):** The Architecture tree (L273-289) duplicates CONTRIBUTING.md
  "Repository layout"; `make` targets appear in Quickstart (L94-108), Plugins block (L246-266), and
  Development (L310-318). Duplication partner: `CONTRIBUTING.md`. Optional: thin the Plugins bash block,
  which restates the Configuration tables (L209-216).
- **Grammar/MLA:** none required (spaced em-dashes and serial commas already consistent).
- **Style note:** Spaced em-dashes (`—`), bold lead-ins on bullets, imperative voice, AGPL/MooseQuest
  attribution. Fix facts only.

### docs/getting-started.md — composite 3.65 (dup 4 / concise 4 / sound 3)
- **Functional (do first — only outright-wrong content in the core docs):**
  - L23: `.../releases/download/v0.3.0/console_v0.3.0_darwin_arm64.tar.gz` → **v0.4.0** (both the path
    segment and the tarball filename).
  - L24: `.../releases/download/v0.3.0/SHA256SUMS.txt` → **v0.4.0**.
  - L26: `tar xzf console_v0.3.0_darwin_arm64.tar.gz && cd console_v0.3.0_darwin_arm64` → **v0.4.0**
    (tarball name **and** extracted dir). Verify: release tag is v0.4.0.
  - Leave L18-19 (`releases/latest`) as-is — it is version-agnostic.
  - **Verified correct:** "Go 1.25.11+" (L29); env defaults (`CONSOLE_ADDR` = `127.0.0.1:8080`,
    `CONSOLE_DB`, `CONSOLE_LLM_PLUGIN`, `CONSOLE_MODEL`); AI-mode plugin names; `qr` command.
- **Coherence (minor, optional):** repo URL case is mixed — `MooseQuest/console` in the download URLs
  (L23-24) vs `moosequest/console` in the clone (L35). The Go module path is lowercase
  `github.com/moosequest/console`. Normalize to lowercase for consistency; not a defect.
- **Style note:** Imperative step headers, breadcrumb intro, blockquote tips. Preserve.

### docs/architecture.md — composite 4.40 (dup 5 / concise 4 / sound 4)
- **Functional:**
  - L31: "`cmd/console-plugin-*/    the 10 out-of-process plugin executables`" → **12**.
  - L44: "`internal/notify/{slack,webhook,email}/   plugin notifier sinks`" →
    `internal/notify/{slack,webhook,email,discord,pagerduty}/`. Verify: `ls internal/notify`.
  - L42 `internal/status/{cloudflare,heroku,sentry}/` is correct — leave.
- **Redundancy / Grammar:** none material; TOC anchors resolve; the two ASCII data-flow diagrams are
  load-bearing — keep.
- **Style note:** Preserve the em-dash voice, the one-way-dependency paragraph, the seam table, and the
  diagrams.

### CONTRIBUTING.md — composite 3.35 (dup 3 / concise 4 / sound 3)
- **Functional:**
  - L3: "Thanks for your interest in Console (**v0.3.0**)." → **v0.4.0** (or drop the inline version to
    avoid future drift). Verify: README badge / git tag.
  - L61: "**10** out-of-process plugin binaries" → **12**.
  - **Verified correct:** "Go 1.25.11+" (L35) and the four env-var seams (L102-106).
- **Redundancy (optional):** "Repository layout" (L59-76) duplicates the README Architecture tree and
  `architecture.md`; could defer to `architecture.md`. Duplication partner: `README.md` / `architecture.md`.
- **Style note:** Tight imperative contributor voice, "(PR)" acronym defined on first use. Preserve.

### docs/security/runtime-hardening.md — composite 4.60 (dup 5 / concise 4 / sound 5)
- **Functional:**
  - §3 SSRF and egress (≈L108-114): the outbound-caller list names only Slack and webhook. Add the
    other shipped HTTP/SMTP-calling sinks — **`discord`** and **`pagerduty`** (and note `email`'s SMTP
    egress) — as outbound-egress surfaces. Verify: `internal/notify/{discord,pagerduty}/` exist.
  - §2 Secrets, log-scrubbing (≈L73-76): scoped to "Slack and webhook plugins." Generalize to
    "URL-/credential-bearing notify sinks" or explicitly add discord (secret webhook URL) and pagerduty
    (routing key).
  - **Verified correct, do NOT change:** the CSP string (L175) is character-for-character identical to
    `internal/server/server.go`; all four header rows (L163-169) match source; Permissions-Policy present.
- **Grammar/MLA:**
  - L187: "a **integrity=** SRI hash" → "**an** integrity= SRI hash" (a → an before a vowel sound).
  - L38-42: run-on joining "Keep it on `127.0.0.1` … via an SSH tunnel, place it behind a reverse
    proxy …, or run it inside a private network segment." Replace the comma splices with semicolons or an
    "either … or" list for readability. (Clarity, not a hard error.)
- **Reduction (optional):** §4 CSRF (L144-153) is verbose for an "academic today" risk — compress the
  per-session-token detail to one sentence. The closing summary table (L225-235) partly restates each
  section's status line.
- **Style note:** Numbered priority sections, "Problem → Done/Planned → operator stopgap" rhythm,
  ✅/🔜 markers, blunt "#1 priority" framing. Preserve.

### docs/security/supply-chain.md — composite 4.40 (dup 5 / concise 4 / sound 4)
- **Functional:**
  - L113/L121: the illustrative action-pin comments (`actions/checkout@abc1234… # v4`, `# v4.2.2`) are
    stale — v0.3.1 bumped the real workflow to **checkout@v6 + setup-go@v6**. Update the example comments
    to `# v6`. Verify: `.github/workflows/ci.yml`.
  - L151: "`git tag -v v0.3.0`" → a current tag (**v0.4.0**).
  - **Verified correct:** htmx-still-on-CDN "🔜 Planned" status (the `unpkg.com` CSP exception is still
    present in source) — leave.
- **Grammar/MLA:** L187/L202 region: "a **integrity=** SRI hash" → "**an** integrity=".
- **Completeness (minor):** "SOP" in the title/body is never expanded (Standard Operating Procedure);
  expand on first use. Verify the relative links `../../CONTRIBUTING.md` (L21) and `../architecture.md`
  (L132) resolve.
- **Style note:** Imperative checklist/SOP voice, the "four questions" dependency gate, triage tables.
  Preserve.

### docs/business/license-analysis.md — composite 4.05 (dup 5 / concise 3 / sound 5)
- **Functional:**
  - L358: "The repo is at **v0.3.0** and still early (0.x)" → **v0.4.0**. **Only** the repo-version
    reference. Do **NOT** change the decision-date stamps ("Decision (2026-06-15)", "Last updated:
    2026-06-15") — those legitimately record when the decision was made.
- **Completeness / Grammar:**
  - "RSALv2" (L168, L300) is never expanded — expand on first use to "Redis Source Available License v2
    (RSALv2)". ("DCO", "CLA", "ASP loophole" are already expanded.)
  - L17 TL;DR: "Staying on MIT would maximize adoption…" reads as an open question although the AGPL
    decision is already made (banner, L3). Optional: past-conditional framing for coherence. Style, not
    a hard error.
- **Reduction (optional, largest in the set):** the per-license **Detailed Profiles** (L55-173), the
  **Comparison Table** (L177-187), and the **Decision Matrix** (L314-321) cover the same licenses on the
  same axes; the **Sources** appendix (L392-442) re-lists every inline citation. Defensible for a
  legal-research artifact — reduce only if the maintainer wants it tighter.
- **Publication check:** confirm `docs/business/` is **not** served by GitHub Pages (Pages builds from
  `docs/index.html`); the file is clean of `kris@` either way.
- **Style note:** Blockquote disclaimer banners, "not legal advice" refrain, source-per-section
  discipline, neutral non-advisory voice. Preserve.

### docs/security/plugin-trust.md — composite 4.40 (dup 5 / concise 4 / sound 4)
- **Functional:**
  - L121: "Today the host passes **`os.Environ()`** to each plugin subprocess." — **unverified.**
    `grep -rn 'os.Environ' internal/` returns nothing; `internal/plugin/host.go:40` uses
    `Cmd: exec.Command(path)` with no `Env` override, so the subprocess inherits the host environment via
    `exec.Command`'s default behavior — the host never calls `os.Environ()`. Soften to: "the plugin
    subprocess inherits the host environment (go-plugin runs `exec.Command` without overriding `Env`)."
    The surrounding "inherits host env by default" framing (L33-39) is correct.
  - **Verified correct:** `AutoMTLS: true` (`internal/plugin/host.go`), `ProtocolVersion: 1`
    (`internal/plugin/plugin.go`).
- **Completeness:** L234 "Surface any design feedback on the parent tracking issue" is a dangling
  reference (no link/issue number) — either add the link or generalize.
- **Reduction (optional):** the "Attack surface summary" table (L63-69) restates the "What it does NOT
  do" prose; trim one.
- **Style note:** ✅/🔜 status markers, "golden rule" + checklist voice, second-person operator address.
  Preserve.

### Docs reviewed and judged clean (no required edits)
- **`docs/development.md`** — composite 5.00. Current at v0.4.0; the cluster's correct reference for
  "Go 1.25.11+" and the four env-var seams. No changes.
- **`docs/api.md`** (4.20), **`docs/flags.md`** (3.80), **`docs/status.md`** (3.80) — all
  source-verified correct (endpoints, reason codes, state ints, scopes, provider keys). Only systemic
  redundancy: the HTTP API endpoint tables appear in all three (api.md canonical, flags.md ≈L165-172,
  status.md ≈L207-215). **Optional** reduction: replace the flags.md/status.md copies with their existing
  "see the full API reference" links. Not required.
- **`docs/onboarding.md`** (4.40) — source-verified correct; no changes.
- **`docs/plugins.md`** (4.60) & **`docs/plugins-architecture.md`** (4.30) — **VERDICT: keep both.**
  They are cleanly split: `plugins.md` = authoring guide ("how to write a plugin"); `plugins-architecture.md`
  = canonical mechanics + the 12-plugin catalog. They already cross-link correctly. **One minor optional
  fix:** `plugins-architecture.md` L175-185 restates the 4-step authoring list that `plugins.md` owns in
  detail — collapse it to a one-line pointer ("To author a plugin, see [plugins.md](plugins.md)").
- **`CLA.md`** (4.00) — clean; correct `support@moosequest.net`. Verify the cross-link
  `docs/business/license-analysis.md §8` resolves.
- **`SECURITY.md`** (4.60) — clean and current. **One minor grammar fix:** L27 uses "CSRF" without
  expansion while "SSRF" is spelled out on first use (L26-27); expand to "cross-site request forgery
  (CSRF)" for parity.

---

## Execution notes for the fixer (LLM/agent)
- Apply edits **in place**; preserve each doc's existing style, voice, and formatting (see per-doc
  **Style note**). Mechanics and facts only — do **not** rewrite voice or add new sections beyond what is
  specified.
- **Propagate from the current docs:** `plugins-architecture.md` (catalog/count) and `development.md`
  (Go version) are correct at v0.4.0 — use them as the reference when fixing counts and lists elsewhere.
- **Verify each functional item before applying:**
  - Plugin count: `ls -d cmd/console-plugin-* | wc -l` → **12**.
  - Notify sinks: `ls internal/notify` → `discord, email, pagerduty, slack, webhook` (**5**).
  - Current release tag → **v0.4.0** (for getting-started.md, CONTRIBUTING.md, license-analysis.md, supply-chain.md).
  - `os.Environ` claim: `grep -rn 'os.Environ' internal/` (expect none) and read `internal/plugin/host.go`.
  - Action pins: `.github/workflows/ci.yml` (expect `checkout@…v6`, `setup-go@…v6`).
- **Do NOT touch:** decision-date stamps in `license-analysis.md` (2026-06-15); the verified-correct CSP
  string and security headers in `runtime-hardening.md`; the `releases/latest` link in `getting-started.md`.
- Treat the "optional" reduction/redundancy items as maintainer's discretion — the **functional** and
  **grammar** items are the required set.
- After edits, run a link-check across `docs/**` and confirm no internal anchors broke; re-render
  `docs/index.html` if the docs site is rebuilt.
