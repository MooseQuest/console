# Documentation Remediation Plan — 2026-06-19

> Produced by the **librarian** (review-only). This plan proposes fixes; it does
> not change the reviewed docs. Point an LLM/agent at it to execute
> ("implement docs/librarian/remediation-2026-06-19.md"). Apply edits **in place**
> and **preserve each doc's existing voice/formatting**.

## Summary

- **Corpus reviewed:** 18 files — every tracked `*.md` except `CHANGELOG.md`
  (auto-release notes, excluded by policy). **Skipped:** `docs/index.html`
  (HTML landing page, outside the prose globs — worth a separate glance for the
  same version/stale-framing issues), `LICENSE` (license text), and the local
  gitignored `CLAUDE.md`.
- **Weights used (default):** Duplicate/redundant **0.40**, Clear & concise
  **0.35**, Objectively sound but subjectively clear **0.25**. Flag if composite
  **< 3.5** or any single criterion **≤ 2**.
- **Flagged for remediation:** 14 (the rest are copy-edit-only or canonical).
- **Buckets:** Duplicates/merges ~7 · Reductions ~10 · Grammar/MLA ~8 docs ·
  **Functional (higher-stakes) ~14**.

The dominant problem is **functional drift**, not prose quality: the docs predate
the move to **out-of-process gRPC plugins** (four seams) and several releases.
Three systemic threads run through almost every flagged doc — fix these first.

### Systemic threads (fix once, everywhere)

1. **Version drift.** Latest release/badge is **v0.3.0**, but docs say "v0.1"
   (`README.md` L26, `CONTRIBUTING.md` L3, `license-analysis.md` "v0.1"),
   download URLs say **v0.2.1** (`README.md` Install), `supply-chain.md` shows
   `git tag -v v0.2.0`, and the security SOPs say "✅ Done (v0.2.x)" (shipped in
   v0.2.1). Normalize all doc references to **v0.3.0** (and "v0.2.1" where naming
   the security release precisely). *Code follow-up (not a doc edit):* the binary
   still reports `version = "0.1.0-dev"` (`cmd/console/main.go:17`).
2. **`CONSOLE_ADDR` default mis-documented.** README config table (L205) and
   `getting-started.md` (L43) say default `:8080`; the actual default is
   **`127.0.0.1:8080`** (`internal/config/config.go:56`) — the prose elsewhere
   already says "loopback by default," so the tables contradict their own docs.
3. **Stale in-process plugin model.** `docs/plugins.md`, `docs/architecture.md`,
   and `CONTRIBUTING.md` still teach "three seams … implement the interface, then
   register it in `internal/app/app.go`." Reality: **four** seams (store, status,
   **notify**, llm), **out-of-process** `console-plugin-*` binaries selected by
   `CONSOLE_*_PLUGIN(S)`. Canonical doc is **`docs/plugins-architecture.md`** —
   four docs link to the stale `plugins.md` instead (`README.md` L297,
   `CONTRIBUTING.md` L103, `getting-started.md` L149, `status.md` L39); repoint
   them (or clearly label `plugins.md` as legacy after it's rewritten).
   Related: `CONSOLE_LLM_PROVIDER` (legacy, ignored by `app.go`) appears in
   `onboarding.md` and `getting-started.md`; AI mode is enabled by
   **`CONSOLE_LLM_PLUGIN`**. *Code follow-up:* `config.LLMProvider` is dead, and
   `CONSOLE_LLM_PROVIDER` still surfaces in `internal/onboard/plan.go:155` and
   `cmd/console/main.go:36`.

## Priority order

1. **docs/plugins.md** — actively wrong + self-contradictory (composite 1.85)
2. **docs/architecture.md** — wrong seam model, moved package, stale tree (2.65)
3. **docs/onboarding.md** — wrong AI-mode mechanism (3.2)
4. **docs/getting-started.md** — wrong AI-mode/env + stale links (3.2)
5. **docs/security/runtime-hardening.md** — wrong header values + env var (3.05)
6. **CONTRIBUTING.md** — stale "Adding a plugin" (3.4)
7. **docs/security/supply-chain.md** — wrong artifact name + stale tag (3.35)
8. **docs/notifications.md** — stale lede + broken TOC anchor (3.6)
9. **README.md** — version/URL/default/framing drift (3.9; functional)
10. **docs/business/license-analysis.md** — MIT-vs-AGPL contradiction (3.95)
11. **docs/api.md** — missing security/conventions note (4.3)
12. **docs/status.md** — stale link + small reductions (4.0)
13. **docs/security/plugin-trust.md** — `SHA256SUMS.txt` fix (3.50)
14. **docs/development.md** — Go version floor mismatch (4.05)
15. **docs/flags.md** — minor dedupe (4.5)
16. **docs/plugins-architecture.md** — canonical; optional polish (4.55)
17. **SECURITY.md** — copy-edit only (4.7)
18. **CLA.md** — verify one link; otherwise none (4.6)

---

## Per-document findings

### docs/plugins.md — composite 1.85 (dup 1 / concise 3 / sound 1)
- **Redundancy:** HIGH — partner is `docs/plugins-architecture.md` (canonical, correct). This is the obsolete in-process guide for the same seams.
- **Functional (fix first):**
  - L12-14: "Adding a plugin is always the same two steps: **implement the interface**, then **wire it in** at the composition root (`internal/app/app.go`)." → WRONG. Correct: implement the seam, build a `cmd/console-plugin-<name>` that calls the seam's `Serve` helper, point the host at it via `CONSOLE_*_PLUGIN(S)`. Nothing is hand-wired in `app.go`.
  - L86-97: example `Status: status.New(st, st, &status.HTTPProvider{}, &status.TCPProvider{})` → actual `app.go:53` is `status.New(st, st, &status.HTTPProvider{})`; plugin providers are added via `a.Status.Register(p)` from `plugin.LoadStatusProvider` (`app.go:96-103`).
  - L218-240: the `newLLM(cfg)` switch and `CONSOLE_LLM_PROVIDER` are fabricated vs current code — no `newLLM` exists; LLM is `plugin.LoadLLM(cfg.LLMPlugin)` (`app.go:105-111`), env `CONSOLE_LLM_PLUGIN` (a path).
  - L279-290: `newNotify`/`cfg.SlackWebhookURL` in-process registration → notifiers are out-of-process via `CONSOLE_NOTIFY_PLUGINS` (`app.go:87-94`).
  - L9: "default LLM = Anthropic" implies compiled-in; no LLM is built in (nil without a plugin).
  - **Verify:** read `internal/app/app.go` (loadPlugins, lines 53/87-94/96-103/105-111); `ls cmd/console-plugin-*`; `grep CONSOLE_LLM internal/config/config.go`.
- **Style note:** Keep the seam table (L5-10) and the "Conventions" section (doc-comment style, stdlib-first, cgo-free, add tests) — accurate, keep verbatim.
- **Proposed action (highest priority):** REWRITE as an authoring guide that defers all mechanics to `plugins-architecture.md` — keep only the (corrected) seam table + Conventions, replace every `app.go`-wiring block with the `cmd/console-plugin-*` + `Serve` template, and adopt the 4-step "Writing a plugin" spine from `plugins-architecture.md`. (Alternative: delete and redirect.) Confirm the seam table lists **four** seams.

### docs/architecture.md — composite 2.65 (dup 4 / concise 4 / sound 1)
- **Functional (fix first):**
  - L51-53: "Three small interfaces are the extension points … implementing the interface and wiring it in `internal/app/app.go`." → **four** seams, out-of-process, loaded via `plugin.LoadStore/LoadStatusProvider/LoadNotifier/LoadLLM` in `app.go` `loadPlugins`.
  - L37: "internal/llm/ LLM Provider interface + Anthropic implementation" → Anthropic **moved**; `internal/llm/` now holds only `provider.go`; impls are `internal/llm/{anthropic,openai,ollama}/`. **Verify:** `ls internal/llm`.
  - L84-96: seam list omits `notify.Notifier` entirely; add it.
  - L103-109 data-flow: shows `sqlite.Open(cfg.DB)` and `newLLM(cfg)` → actual `openStore` branches on `cfg.StorePlugin` vs SQLite; LLM is `plugin.LoadLLM(cfg.LLMPlugin)`; no `newLLM`. **Verify:** `internal/app/app.go:75-114`.
  - Package layout L27-43 omits `internal/notify/`, `internal/plugin/`, `internal/store/postgres/`, `internal/status/{cloudflare,heroku,sentry}/`, `internal/llm/{anthropic,openai,ollama}/`, and the 10 `cmd/console-plugin-*` dirs. Opening (L3-5) "all over one embedded SQLite database" ignores pluggable storage.
- **Reduce:** Cut the inline `Store`/`Provider` Go bodies (L57-96) — restate source/plugins.md; keep prose.
- **Grammar/MLA:** L16 "no dependencies" vs L22 "minimal dependencies" — reconcile wording.
- **Style note:** Preserve the "why," narrative voice and the "one binary you can scp to a box" closing.
- **Proposed action:** REWRITE the "interface seams" + "data flow" sections to the four-seam out-of-process model; refresh the package tree; fix the anthropic location; add a link to `plugins-architecture.md` as canonical for plugin mechanics.

### docs/onboarding.md — composite 3.2 (dup 4 / concise 3 / sound 3)
- **Functional (fix first):** AI mode is enabled by **`CONSOLE_LLM_PLUGIN`** (path to `console-plugin-anthropic|openai|ollama`); `app.go` builds `a.LLM` only from `cfg.LLMPlugin` (L105-113) and gates AI mode on `a.LLM == nil` (`onboard_cmd.go:48`).
  - L80-82: "It is configured by `CONSOLE_LLM_PROVIDER` (default `anthropic`; set to `""` to disable)" → WRONG; `CONSOLE_LLM_PROVIDER` is read into config but **never consumed**. Setting `""` does not disable AI mode.
  - L85-88: the `CONSOLE_LLM_PROVIDER="" console onboard` "disable" example is non-functional — AI mode is "disabled" simply by not setting `CONSOLE_LLM_PLUGIN`.
  - L21 table cell "a configured LLM provider + `ANTHROPIC_API_KEY`" → "a configured LLM **plugin** (`CONSOLE_LLM_PLUGIN`) + the matching key (e.g. `ANTHROPIC_API_KEY`)." (L56-67 already gets this right — the doc contradicts itself; keep 56-67.)
  - **Verify:** `grep -rn "LLMProvider\|LLMPlugin" internal/app/app.go internal/config/config.go cmd/console/onboard_cmd.go`.
- **Reduce (~25%):** The AI-config story is told three times (L21, L56-59, L80-88); merge into one. CLI reference (L118-141) repeats inline flag descriptions — keep the table, drop the second worked example (L132-141).
- **Style note:** Preserve the two-mode comparison table and the "both modes produce the same `Plan`" + idempotent-apply framing.
- **Proposed action:** Rewrite the AI-Assisted section onto `CONSOLE_LLM_PLUGIN`; delete the `CONSOLE_LLM_PROVIDER` disable example (L80-88); fix the L21 cell; reduce ~25%.

### docs/getting-started.md — composite 3.2 (dup 3 / concise 3 / sound 2)
- **Functional (fix first):**
  - L45/L47 Configure table: `CONSOLE_LLM_PROVIDER` → replace with **`CONSOLE_LLM_PLUGIN`** (path to plugin binary); the key is read but ignored by `app.go`.
  - L130-137 AI-onboarding example sets only `ANTHROPIC_API_KEY` — **add** `export CONSOLE_LLM_PLUGIN=$PWD/bin/console-plugin-anthropic` (as `README.md` L189 does); without it AI mode won't enable.
  - L43 `CONSOLE_ADDR` default `:8080` → **`127.0.0.1:8080`**.
  - L149 `[Plugins](plugins.md)` → repoint to `plugins-architecture.md`.
- **Completeness:** No prebuilt-binary install path (newcomers without Go); add it (README leads with prebuilt). No `console qr` / phone note; consider adding.
- **Redundancy/Reduce (~20%):** First-flag/status/serve blocks duplicate README Quickstart — this doc should own the long-form tutorial; README should defer here.
- **Style note:** Preserve the `→` output annotations, `>` tip blockquotes, and the install→flag→check→serve→onboard spine.
- **Proposed action:** Targeted functional fixes + add prebuilt install + reduce README overlap ~20%.

### docs/security/runtime-hardening.md — composite 3.05 (dup 3 / concise 3 / sound 3)
- **Functional (fix first — these are correctness errors, not optional):**
  - L31: "must set `CONSOLE_BIND`" → no such var; it's **`CONSOLE_ADDR`** (`internal/config/config.go`, default `127.0.0.1:8080`).
  - §5 table `Referrer-Policy = strict-origin-when-cross-origin` → code sets **`no-referrer`** (`internal/server/server.go:96`).
  - §5 table lists `Permissions-Policy` as set → it is **not** set (middleware sets only X-Content-Type-Options, X-Frame-Options, Referrer-Policy, CSP — `server.go:94-98`). Remove the row or mark it 🔜 Planned.
  - §5 prose/table call the CSP "permissive baseline"/"weak CSP" → it is **restrictive**: `default-src 'self'; script-src 'self' https://unpkg.com; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'` (`server.go:97-98`). Reword to "restrictive CSP with a single CDN exception for htmx."
  - **Verify:** read `internal/server/server.go:91-99`, `internal/config/config.go`.
  - (All other ✅ claims verified accurate: loopback default, AutoMTLS `host.go:42`, server timeouts, 1 MiB cap, LLM LimitReader, RunAll semaphore 16, URL redaction.)
- **Reduce (~15%):** Collapse §4 CSRF to 2-3 sentences (academic until auth). Trim §1 "Acceptable patterns" overlap with SECURITY.md; dedupe §6 reverse-proxy advice against §1.
- **Grammar/MLA:** L7 "honestly and in priority order" → "in priority order." Preserve the parallel bold lead-ins in §6.
- **Style note:** Preserve the candid, priority-ordered voice and the Problem → Done/Planned → operator-fallback structure.
- **Proposed action:** Copy-edit the four §5/env correctness errors; reduce ~15%; tighten "v0.2.x" → "v0.2.1" where precise.

### CONTRIBUTING.md — composite 3.4 (dup 4 / concise 4 / sound 2)
- **Functional (fix first):**
  - L3 "early (v0.1) project" → v0.3.0.
  - L97-107 "## Adding a plugin": "The three extension seams are `store.Store`, `status.Provider`, and `llm.Provider`. Adding one is always: implement the interface, then register it in `internal/app/app.go`." → WRONG. **Four** seams (add **notify**), out-of-process `console-plugin-*` binaries selected by env; new plugins are built via `make plugins`. Repoint L103 from `docs/plugins.md` to `docs/plugins-architecture.md`.
  - **Verify:** `ls cmd/ | grep console-plugin` (10), `grep -rn 'CONSOLE_.*_PLUGIN' internal/config/config.go`.
- **Completeness:** Repo-layout tree (L58-73) disagrees with README's tree (CONTRIBUTING lists `internal/config`, `internal/web`; README omits them). Make both consistent and both defer to `docs/architecture.md`. Expand "PR" once.
- **Style note:** Preserve the top-TOC, the **bold-lead** convention in Coding conventions, and the "explain *why*" emphasis.
- **Proposed action:** Rewrite "## Adding a plugin" to the four-seam out-of-process model + repoint; fix v0.1→v0.3.0; reconcile the layout tree with README. Otherwise copy-edit.

### docs/security/supply-chain.md — composite 3.35 (dup 3 / concise 4 / sound 3)
- **Functional (fix first):**
  - L151 `git tag -v v0.2.0` → **`v0.3.0`** (latest).
  - L157/L160/L228 + plugin section: `SHA256SUMS` / `sha256sum -c SHA256SUMS` → the released file is **`SHA256SUMS.txt`** (`CHANGELOG.md:19`). Fix filename and command.
  - L49: "CI runs with `-mod=readonly`" → not explicit in `.github/workflows/ci.yml`; Go defaults to readonly with a committed `go.sum`. Soften to "CI builds with a committed `go.sum` (Go's default `-mod=readonly`)," or add the flag. **Verify:** read `ci.yml:28-33`.
  - (All other ✅ claims verified: govulncheck CI job, Actions pinned to 40-char SHAs, Dependabot present.)
- **Redundancy/Reduce (~10%):** The **htmx vendor + SRI** plan is told here *and* in `runtime-hardening.md §5` — assign one canonical home (supply-chain owns vendoring/integrity; runtime-hardening owns CSP) and cross-link. Trim checklist overlap (govulncheck in all three checklists).
- **Grammar/MLA:** British spellings ("analyses", "recognises") vs US elsewhere — normalize to **US** to match the repo register.
- **Style note:** Preserve the triage table, the four-gate dependency checklist, and the "a `go.sum`-only diff is a red flag" maxim.
- **Proposed action:** Copy-edit the three functional items; reduce ~10%; normalize spelling.

### docs/notifications.md — composite 3.6 (dup 4 / concise 4 / sound 3)
- **Functional (fix first):**
  - Lede L5-6 frames webhook/email as hypothetical "natural additions behind the same interface," and the TOC lists only "[The Slack notifier]" — but **slack/webhook/email all ship as plugins** (`cmd/console-plugin-{slack,webhook,email}`, via `CONSOLE_NOTIFY_PLUGINS`). The L52-56 blockquote already says so → the doc contradicts itself. Rewrite the lede to "all three ship as plugins."
  - L86-87 hypothetical/future tense ("A webhook notifier *would* POST…") → present ("`console-plugin-webhook` POSTs each event as JSON; `console-plugin-email` formats it for SMTP").
  - **Verify:** `ls cmd/console-plugin-*`; `internal/app/app.go:86-95`. (Emission semantics at L58-71 are correct.)
- **Grammar/MLA:** **Broken TOC anchor** — TOC L9 `[The Slack notifier](#the-slack-notifier)` but the heading is "## The Slack notifier plugin" (anchor `#the-slack-notifier-plugin`). Align heading and anchor.
- **Reduce (~20%):** Trim "Writing a notifier" (L73-89) to the `Notifier` interface + a one-line "build it as a `console-plugin-<name>`; see plugin architecture."
- **Style note:** Preserve the event-type table, the `Severity()` color mapping, and the ASCII emission diagram.
- **Proposed action:** Rewrite lede/TOC + "Writing a notifier" to present-tense, all-three-ship framing; fix the TOC anchor; reduce ~20%.

### README.md — composite 3.9 (dup 5 / concise 3 / sound 3)
- **Functional (fix first):**
  - L26 "Status: **early / v0.1**." → **v0.3.0** (badge already says v0.3.0).
  - L205 config table `CONSOLE_ADDR` default `:8080` → **`127.0.0.1:8080`** (contradicts its own L65 "binds to loopback").
  - L50-61 Install download URLs/tarball names pin **v0.2.1** → bump to **v0.3.0**.
  - L31 "Storage, status providers, and LLM providers all sit behind small Go interfaces — swap or extend them without touching the core." → stale "three seams/compiled-in" framing; reword to the **four out-of-process seams** (as L234-241 correctly states).
  - L271 CLI list "(serve, flag, status, onboard)" omits **`qr`** (and `version`); add `qr`.
  - L270-281 Architecture tree omits `internal/config`, `internal/notify`, `internal/plugin`, `internal/web`; update.
- **Redundancy/Reduce:** `### Plugins` (L232-266) restates the `## Configuration` plugin tables (L208-231) and the Install plugin note — cut the prose paragraph (L234-241), keep the `make build && make plugins` block + a pointer to `plugins-architecture.md`. Trim `### Open it on your phone` (~40%) and keep README Quickstart short, deferring depth to `getting-started.md`.
- **Grammar/MLA:** Split the multi-clause run-ons at L139 and L195 for readability (no hard MLA error).
- **Completeness:** L297 links `docs/plugins.md` as "Writing plugins" — point to `plugins-architecture.md` (or relabel once plugins.md is rewritten).
- **Style note:** Preserve the centered header `<div align="center">`, the badge row, the em-dash voice, and the `> Status:` blockquote.
- **Proposed action:** Copy-edit + the functional fixes above; reduce `### Plugins` ~50% to a pointer.

### docs/business/license-analysis.md — composite 3.95 (dup 4 / concise 4 / sound 4)
- **Functional (fix first):** The top **Decision banner (2026-06-15)** says the license is now AGPL-3.0, but the body still calls MIT current: L25 "Current placeholder license: **MIT**", L278 "Console is currently under MIT", L370 "currently has a MIT LICENSE file", and the §5 matrix "MIT (current)" (L317). Ground truth: **AGPL-3.0 + CLA**. Convert these to past tense ("was MIT prior to the 2026-06-15 decision") or add a one-line reconciliation. Also L356/§7 "The repo is at v0.1" → v0.3.0 (or "still early (0.x)").
- **Reduce (~5% max — it's a reference/decision record, do not over-cut):** §4 "Pattern observation" bullets (L300-304) restate the comparison table's adoption column — trim ~40% of that sub-bullet only. Keep the full "Sources" list (audit trail).
- **Grammar/MLA:** Split the 4-line TL;DR sentence (L17) at "AGPL-3.0 flips that default…".
- **Style note:** Preserve the "research-only, not legal advice" framing, the hedges, the source-cited rigor, and the decision-matrix-by-intent structure.
- **Proposed action:** Reconcile the "current = MIT" framings to past tense; update "v0.1" → v0.3.0; split the TL;DR; otherwise copy-edit only.

### docs/api.md — composite 4.3 (dup 4 / concise 5 / sound 4)
- **Functional/Completeness (add, don't correct):** This is where integrators look, but it omits the now-applied posture. Add a short **Security/Conventions** note: requests are capped at **1 MiB** (oversized rejected — `internal/server/server.go:181`), the server sets **security headers** (nosniff, X-Frame-Options DENY, Referrer-Policy, CSP — `server.go:91-99`), and there is **no API auth yet** (bind to loopback; `CONSOLE_ADDR` default `127.0.0.1:8080`).
- **Redundancy:** Canonical home for endpoint contracts/errors — keep the JS wrapper (L221-236) here; cut the duplicate in `flags.md`.
- **Style note:** Preserve the per-endpoint heading + curl + jsonc-response rhythm and the `core.*` type call-outs.
- **Proposed action:** Add the Security/Conventions note; otherwise copy-edit only.

### docs/status.md — composite 4.0 (dup 4 / concise 4 / sound 4)
- **Functional:** L39 `see [plugins](plugins.md)` → repoint to **`plugins-architecture.md`** (the rest of the doc already links there at L48/L94; `plugins.md` contradicts it). Pair the L32-39 "registered by name" wording with the out-of-process note so it doesn't read as in-process registration. (Provider plugin callouts at L44-48/L92 are accurate.)
- **Grammar/MLA:** L164 "grey" → **"gray"** (US, matches "color"/"behavior" elsewhere).
- **Reduce (~10%):** Trim the "HTTP API" block (L209-231) to a short table + one curl, deferring shapes to `api.md` (already linked L233). Cut the bash add-snippet (L125-129), keep the JSON (L131-138).
- **Style note:** Preserve the per-provider config tables and the blockquote callouts separating built-in from plugin providers.
- **Proposed action:** Fix the link + grey→gray; reduce HTTP section ~10%.

### docs/security/plugin-trust.md — composite 3.50 (dup 4 / concise 4 / sound 3)
- **Functional:** L107/L152/L165 reference `SHA256SUMS` → **`SHA256SUMS.txt`** (matches release/`CHANGELOG.md:19`). (AutoMTLS ✅, SecureConfig 🔜, minimal-env 🔜 all correct.)
- **Redundancy:** The "Golden rule" (L138-142) is near-verbatim with `supply-chain.md`'s "only run plugins you built yourself" — keep it here (canonical home for plugin trust), have `supply-chain.md` link rather than restate.
- **Completeness:** It references a "Reads column" in `plugins-architecture.md` (L129-131, 188, 215) — confirm that column exists there (it does: the plugin catalog table).
- **Style note:** Preserve the ✅/🔜 markers, the "What it does / does NOT do" framing, and the "There is no sandbox" honesty.
- **Proposed action:** Copy-edit only (`SHA256SUMS.txt`; optional golden-rule dedupe).

### docs/development.md — composite 4.05 (dup 5 / concise 4 / sound 4)
- **Functional:** L17 "**Go 1.22+**" → `go.mod` declares `go 1.26.4` (`grep '^go ' go.mod`), so a Go 1.22 user can't build. **Decision needed:** either correct the doc to the real floor (**Go 1.26+**) **or** lower the `go.mod` directive to `1.22` if the code only needs 1.22 features (recommended if true — keeps the contributor bar low). Pick one so the doc and `go.mod` agree. *(This straddles doc + code; verify against `go.mod` and whether any 1.23+ stdlib feature is used.)*
- **Style note:** Preserve the per-OS parallel blocks (macOS/Linux vs Windows PowerShell) and the "no special setup" reassurance.
- **Proposed action:** Reconcile the Go-version floor (one-line doc fix, or a `go.mod` change + keep "1.22+"). Otherwise leave as-is — Makefile targets and commands all verified.

### docs/flags.md — composite 4.5 (dup 5 / concise 4 / sound 5)
- **Reduce (~10%):** The JS `evaluate()` snippet (L185-200) duplicates `api.md` (L221-236) almost verbatim — cut it here, link to `api.md`; keep the one-line curl.
- **Grammar/MLA:** L85 mixes interval notation with a chained inequality — minor; consider "every bucket is in `[0,100)`, so `< 100`."
- **Functional:** None — accurate (scopes, FNV rollout, multivariate, reason codes, "no native SDK yet").
- **Style note:** Preserve the bold-term-on-first-mention convention and the jsonc commented-schema blocks (the doc-set signature).
- **Proposed action:** Reduce ~10% (dedupe JS snippet); else copy-edit only.

### docs/plugins-architecture.md — composite 4.55 (dup 5 / concise 4 / sound 5) — CANONICAL
- **Functional:** None — verified against `internal/plugin/host.go`, `internal/app/app.go`, `proto/`; the 10-plugin catalog matches `cmd/` exactly; env names correct.
- **Reduce (optional):** The three "Using the … plugins" sections (L64-137) repeat the `make build && make plugins` preamble — share one preamble. Optional: add one line noting **AutoMTLS is on** (`host.go:42`).
- **Style note:** Preserve the "Terraform/Vault model" framing and the "How it fits together" ASCII diagram.
- **Proposed action:** KEEP as canonical. Make `plugins.md` and `architecture.md` defer here. Optional minor dedupe + AutoMTLS line.

### SECURITY.md — composite 4.7 (dup 5 / concise 5 / sound 4)
- **Functional:** None — matches ground truth (no built-in auth, loopback default `127.0.0.1`, latest-release support, correct `support@moosequest.net`).
- **Grammar/MLA:** Expand **"SOP"** → "Standard Operating Procedures (SOPs)" on first use (L21 heading). Optionally expand **"SSRF"** on first use.
- **Style note:** Preserve the blockquote auth warning, the "Start here." nudge, and the firm-but-reassuring tone.
- **Proposed action:** Copy-edit only.

### CLA.md — composite 4.6 (dup 5 / concise 5 / sound 4)
- **Functional:** None. Verify the link target `docs/business/license-analysis.md` exists (it does) — no change needed if so.
- **Style note:** Preserve the ⚠️ DRAFT blockquote, the **bold-quote** defined-term convention, the numbered legal structure, and the closing italic contact line.
- **Proposed action:** None beyond confirming the link resolves.

---

## Execution notes for the fixer (LLM/agent)

- Apply edits **in place**; preserve each doc's existing **style/voice/formatting**.
  Do **not** introduce new sections beyond what's specified here.
- **Do the three systemic threads first** (version → v0.3.0; `CONSOLE_ADDR`
  default; in-process→out-of-process plugin model + `plugins.md` link repointing)
  — they touch many files; fixing them removes most functional findings at once.
- **Verify every "Functional" item before applying** — each lists a file/command
  to check (`internal/app/app.go`, `internal/server/server.go:91-99`,
  `internal/config/config.go`, `ls internal/llm`, `go.mod`, `.github/workflows/ci.yml`).
  Do not change a value you haven't confirmed against the source.
- **Canonical-home map:** `plugins-architecture.md` owns plugin mechanics;
  `api.md` owns HTTP endpoint contracts/errors/security; `flags.md`/`status.md`
  own their concept models (trim HTTP sections toward `api.md`); `runtime-hardening.md`
  owns the long-form auth/CSP story (SECURITY.md keeps the boxed warning + link);
  `plugin-trust.md` owns the plugin threat model; `supply-chain.md` owns
  dependency/build/vendoring integrity.
- After edits, run a **link check** (no dangling internal links) and re-render the
  docs site / `docs/index.html` if it references any changed content.
- **Normalize spelling to US English** across the set (the only outlier is
  `supply-chain.md`).

## Code follow-ups (out of doc scope — file as cards, do NOT edit as part of this plan)

1. Binary version string `version = "0.1.0-dev"` (`cmd/console/main.go:17`) is
   stale vs the v0.3.0 release — wire it to `git describe` / the release tag.
2. `config.LLMProvider` (`CONSOLE_LLM_PROVIDER`) is read but never consumed —
   dead field; either remove it or make it select the LLM plugin. It also still
   appears in `internal/onboard/plan.go:155` and `cmd/console/main.go:36`.
3. Decide the supported Go floor: `go.mod` says `go 1.26.4`; if only 1.22 features
   are used, lower it (keeps the contributor bar low) — otherwise the docs must
   say "Go 1.26+".
4. (Security roadmap, already tracked in `docs/security/`) `Permissions-Policy`
   header is documented as desirable but not set — add it to the
   `securityHeaders` middleware, or it stays a doc-level 🔜.
