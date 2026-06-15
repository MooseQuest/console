# Feature flags

A feature flag lets you ship code to production but expose it only to a chosen
audience, gradually. Console's flag engine resolves a flag to an **evaluation**
for a given **subject** deterministically — the same `(flag, subject)` pair
always yields the same result, so a percentage rollout is stable across calls,
processes, and restarts.

- [The flag model](#the-flag-model)
- [Scopes](#scopes)
- [Rollout and determinism](#rollout-and-determinism)
- [The evaluation pipeline](#the-evaluation-pipeline)
- [Multivariate flags](#multivariate-flags)
- [CLI](#cli)
- [HTTP API](#http-api)

## The flag model

```jsonc
{
  "key": "new-dashboard",      // unique identifier
  "description": "New dashboard UI",
  "enabled": true,             // master on/off gate
  "scope": "beta",             // audience: all | beta | alpha | cohort | experiment
  "rollout": 50,               // 0..100 — % of in-scope subjects who get it
  "variants": [],              // empty = boolean flag; non-empty = multivariate
  "cohort": "",                // names the cohort when scope == cohort
  "experiment": ""             // names the linked experiment when scope == experiment
}
```

A **subject** is the entity you evaluate for — usually an end user, but it can be
any keyed actor (a service, a tenant). It carries attributes used by scope and
cohort matching:

```jsonc
{ "key": "user-123", "attributes": { "audience": "beta", "cohort": "power_users" } }
```

An **evaluation** is the result:

```jsonc
{
  "flag_key": "new-dashboard",
  "enabled": true,
  "variant": "on",            // "on"/"off" for boolean; the variant key otherwise
  "value": "",                // a multivariate variant's value, when present
  "reason": "rollout_included"
}
```

## Scopes

The scope decides whether a subject is *in scope* at all (before rollout is even
considered).

| Scope | In scope when… |
|---|---|
| `all` | always |
| `beta` / `alpha` | `attributes["audience"]` equals the scope (`"beta"`/`"alpha"`), **or** the attribute named after the scope equals `"true"` (e.g. `attributes["beta"] == "true"`) |
| `cohort` | the flag has a non-empty `cohort` **and** `attributes["cohort"]` equals it |
| `experiment` | always in scope — the linked `experiment` is analysis metadata, not a gate |

Any unrecognized scope fails closed (out of scope). Examples:

```bash
# beta scope — either of these subjects is in scope:
./console flag eval new-dashboard -subject u1 -attr audience=beta
./console flag eval new-dashboard -subject u2 -attr beta=true

# cohort scope — the cohort attribute must match the flag's cohort:
./console flag create checkout-v2 -scope cohort -cohort power_users -rollout 100 -enabled
./console flag eval checkout-v2 -subject u3 -attr cohort=power_users   # in scope
./console flag eval checkout-v2 -subject u4 -attr cohort=free          # out_of_scope
```

## Rollout and determinism

`rollout` is the percentage (0–100) of **in-scope** subjects who receive the
flag. Bucketing is derived from a stable 64-bit **FNV-1a** hash of the string
`"<flag-key>:<subject-key>"`, mapped into `[0, 100)`. A subject is rolled in when
its bucket is **less than** `rollout`:

- `rollout: 0` includes nobody (no bucket is `< 0`).
- `rollout: 100` includes everybody (every bucket is `≤ 99 < 100`).
- `rollout: 50` includes a stable ~50% — the *same* subjects every time.

Because the hash is per `(flag, subject)`, changing one flag's rollout does not
reshuffle another flag's buckets, and the same subject is consistently in or out
across processes. There is no randomness and no server state involved in the
decision.

## The evaluation pipeline

Evaluation runs a series of gates; the first that fails returns a disabled `off`
result whose `reason` names the gate:

1. **Load** — flag fetched by key. Missing → `not found` error.
2. **Enabled gate** — if `enabled` is false → `reason: "flag_disabled"`.
3. **Scope gate** — if the subject is out of scope → `reason: "out_of_scope"`.
4. **Rollout gate** — if the subject's bucket `>= rollout` → `reason: "rollout_excluded"`.
5. **Variant selection** — the subject is rolled in:
   - boolean flag → `variant: "on"`, `reason: "rollout_included"`.
   - multivariate flag → a weighted variant, `reason: "variant"`.

| `reason` | Meaning |
|---|---|
| `flag_disabled` | the flag's master switch is off |
| `out_of_scope` | the subject isn't in the flag's audience |
| `rollout_excluded` | in scope, but above the rollout cutoff |
| `rollout_included` | boolean flag served `on` |
| `variant` | multivariate flag served a specific variant |

## Multivariate flags

A flag with one or more `variants` is multivariate: rolled-in subjects receive
one variant chosen deterministically by weight. The variant is selected with a
**second, independent** hash (`"<flag-key>:variant:<subject-key>"`), so the
rollout decision and the variant choice don't correlate.

```jsonc
{
  "key": "cta-copy",
  "enabled": true,
  "scope": "all",
  "rollout": 100,
  "variants": [
    { "key": "control", "value": "Sign up", "weight": 1 },
    { "key": "urgent",  "value": "Sign up now", "weight": 1 },
    { "key": "value",   "value": "Start free",  "weight": 2 }
  ]
}
```

A subject is mapped onto the cumulative weights (here 1/1/2 → 25% / 25% / 50%).
If every weight is non-positive, the engine falls back to the first variant so a
result is always returned.

## CLI

```text
console flag list
console flag get <key>
console flag create <key> [-desc -scope -rollout -enabled -cohort -experiment]
console flag enable <key>
console flag disable <key>
console flag delete <key>
console flag eval <key> -subject <id> [-attr k=v ...]
```

```bash
# Create a cohort-scoped flag, fully rolled out, enabled
console flag create checkout-v2 -desc "New checkout" -scope cohort -cohort power_users -rollout 100 -enabled

# Evaluate for a subject with attributes (repeat -attr for each)
console flag eval checkout-v2 -subject u-42 -attr cohort=power_users

# Flip the master switch
console flag disable checkout-v2
console flag enable  checkout-v2
```

## HTTP API

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/flags` | List flags |
| `POST` | `/api/flags` | Create a flag |
| `GET` | `/api/flags/{key}` | Get a flag |
| `PUT` | `/api/flags/{key}` | Update a flag |
| `DELETE` | `/api/flags/{key}` | Delete a flag |
| `POST` | `/api/flags/{key}/evaluate` | Evaluate for a subject |

Evaluate (the body is a `core.Subject`; the response is a `core.Evaluation`):

```bash
curl -X POST localhost:8080/api/flags/new-dashboard/evaluate \
  -d '{"key":"user-123","attributes":{"audience":"beta"}}'
# → {"flag_key":"new-dashboard","enabled":true,"variant":"on","reason":"rollout_included"}
```

From application code, the integration is the same HTTP call — there is no
native client SDK yet (it's on the roadmap):

```js
async function evaluate(flagKey, subject) {
  const res = await fetch(`http://localhost:8080/api/flags/${flagKey}/evaluate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(subject), // { key, attributes }
  });
  return res.json(); // { flag_key, enabled, variant, value, reason }
}

const { enabled } = await evaluate("new-dashboard", {
  key: "user-123",
  attributes: { audience: "beta" },
});
if (enabled) renderNewDashboard();
```

See the full [API reference](api.md) for error responses and the flag CRUD
endpoints.
