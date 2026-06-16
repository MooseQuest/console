# Onboarding

Onboarding helps you register an application into Console: which **components**
to monitor and which **feature flags** to create. Both modes produce the same
artifact — a `Plan` — which you can apply to a live Console and/or render as a
Markdown setup guide.

- [The two modes](#the-two-modes)
- [The plan](#the-plan)
- [Human mode](#human-mode)
- [AI-Assisted mode](#ai-assisted-mode)
- [Applying a plan](#applying-a-plan)
- [The generated guide](#the-generated-guide)
- [CLI reference](#cli-reference)

## The two modes

| Mode | How it works | Needs |
|---|---|---|
| **Human** | An interactive terminal wizard walks you through components and flags. | nothing |
| **AI-Assisted** | You describe your app in a sentence; an LLM (Claude by default) drafts the plan. | a configured LLM provider + `ANTHROPIC_API_KEY` |

Both modes are non-destructive on their own: building a plan never touches
storage. Persisting happens only when you apply it.

## The plan

A plan is a pure data value for one application:

- **App** — the display name.
- **Description** — a free-text summary of what it does.
- **Components** — the parts to monitor (each with a key, name, provider, config).
- **Flags** — the feature flags to create.
- **Notes** — advisory messages (guidance while drafting, or skip notices on apply).

## Human mode

```bash
console onboard
```

The wizard flow:

1. App **name**, then app **description**.
2. A **component loop** — for each component it asks for a name (blank ends the
   loop), a provider (default `http`), and a URL / config value.
3. A **flag loop** — for each flag it asks for a key (blank ends the loop), a
   description, a scope (default `all`), and a rollout percentage (default `0`,
   clamped to 0–100).

Input is trimmed; component keys are slugged from the name when you don't supply
one. The wizard reads from stdin and writes prompts to stdout, so it works the
same over a pipe.

## AI-Assisted mode

AI mode needs an LLM plugin (e.g. `console-plugin-anthropic`) selected via
`CONSOLE_LLM_PLUGIN`; the plugin reads `ANTHROPIC_API_KEY` from the environment.
With no LLM plugin configured, AI mode is unavailable and Human mode still works.

```bash
export CONSOLE_LLM_PLUGIN=$PWD/bin/console-plugin-anthropic
export ANTHROPIC_API_KEY=sk-ant-...
console onboard -ai \
  -name "Acme" \
  -desc "A Rails store with a Sidekiq worker and a Postgres DB"
```

In AI mode Console sends your name and description to the configured LLM provider
with a strict system prompt asking for a single JSON object: a list of
components (2–5) and flags (2–5) that fit the description, plus advisory notes.
The reply is parsed robustly (a wrapping ```` ```json ```` fence is stripped) and
normalized:

- rollout clamped to 0–100,
- scope defaulted to `all` when blank/unknown,
- provider defaulted to `http`,
- component keys minted from names when absent.

AI mode requires a provider. It is configured by `CONSOLE_LLM_PROVIDER`
(default `anthropic`; set to `""` to disable) and the matching key
(`ANTHROPIC_API_KEY`). If no provider is configured, AI mode reports a clear
error and you should fall back to Human mode.

```bash
# Disable AI entirely (Human mode still works)
CONSOLE_LLM_PROVIDER="" console onboard
```

## Applying a plan

Pass `-apply` to persist the plan into the store — components are created via the
status engine and flags via the flag engine:

```bash
console onboard -ai -name "Acme" -desc "..." -apply
```

Apply is **idempotent-friendly**: if a component or flag key already exists, that
item is *skipped* (recorded as a note) rather than failing, and the remaining
items still apply. Re-running apply over an already-partially-onboarded app
converges instead of erroring. Any *other* store error aborts immediately.

## The generated guide

Pass `-guide <path>` to render the plan as a deterministic Markdown setup guide —
a title, the description, a Components section, a Feature flags section, any
notes, and a Next steps checklist:

```bash
console onboard -ai -name "Acme" -desc "..." -guide acme-setup.md
```

The guide is generated from the plan alone, so you can produce it without
applying (review first, apply later) — or do both in one run by combining
`-apply` and `-guide`.

## CLI reference

```text
console onboard [-ai] [-name <app>] [-desc <text>] [-apply] [-guide <path>]
```

| Flag | Effect |
|---|---|
| `-ai` | use AI-Assisted mode (otherwise the Human wizard) |
| `-name <app>` | application display name |
| `-desc <text>` | application description (the AI prompt input) |
| `-apply` | persist the plan into the store (skips existing keys) |
| `-guide <path>` | write a Markdown setup guide to `<path>` |

```bash
# Human mode, write a guide but don't apply
console onboard -guide setup.md

# AI mode, draft + apply + guide in one shot
export ANTHROPIC_API_KEY=sk-ant-...
console onboard -ai -name "Acme" \
  -desc "A Rails store with a Sidekiq worker and a Postgres DB" \
  -apply -guide acme-setup.md
```
