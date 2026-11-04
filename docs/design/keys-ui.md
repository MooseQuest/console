# Dashboard UI for API key management — design

Status: implemented · Issue: #105 · Parent: #79 · Builds on: [auth.md](auth.md) sections 3.2 and 4.4, and #101-#104

## 1. Scope and invariants

An admin-only dashboard page to create, list and revoke managed API keys and to
read the key audit log. Binding constraints, from auth.md and the #105 grooming:

- All four routes need the `admin` scope and get `routeScopes` entries
  (`TestRouteScopesCoverMux` enforces this). Anonymous browsers get 303 to
  `/login`; non-admin sessions get the existing 403. No new middleware behavior.
- Every POST is cookie-authenticated, so it passes the per-session CSRF check
  plus `CrossOriginProtection`. Negative tests mirror `csrf_test.go`.
- The token appears only in the body of the create response. Never in a URL,
  redirect, flash cookie, log line, or the list.
- Handlers call `auth.CreateManagedKey` / `auth.RevokeManagedKey` with the same
  `KeyStore` as #104 (not the HTTP API), so scope ceiling, duplicate-name 409,
  future-expiry and the persisted `key.create` / `key.revoke` events (session
  principal name plus request ID) are identical.
- The CSP in `securityHeaders` is unchanged: no inline script, no inline
  style, no `hx-on`, no `style=` attributes.
- Existing visual language only: `panel`, `btn`, `btn-primary`, `muted`,
  `mono`, `login-form`-style fields, `.dot`, `--c-*` tokens. New CSS is limited
  to a few small classes in `console.css` (form grid, secret box, `sr-only`)
  built from existing tokens.

## 2. Key decision: plain forms, not hx-post, for mutations

htmx 2 does not swap 4xx/5xx responses, so validation (400), conflict (409) and
ceiling (403) errors from an `hx-post` form would be silently dropped; the only
fix is a global `htmx-config` change that would alter the existing flag toggles.
Therefore create, revoke-confirm and revoke are **plain `<form method=post>`
with a hidden `csrf_token` field** and full-page responses carrying the correct
status. This also keeps the secret out of any htmx history snapshot by
construction. htmx is used only for the read-only audit fragment (GET). The
create response additionally sets `HX-Push-Url: false` and the page `<main>`
carries `hx-history="false"` (grooming requirement, defence in depth).

## 3. Routes

| Route | Scope | Purpose | Success | Errors |
|---|---|---|---|---|
| `GET /keys` | admin | full page: form, list, audit | 200 | 501 if no KeyStore; 500 |
| `POST /keys` | admin | create from form | 201 page showing the secret | 400, 403 (ceiling), 409 (name), 413, 501 |
| `POST /keys/{id}/revoke` | admin | two-step revoke (section 5) | step 1: 200 confirm page; step 2: 303 to `/keys?revoked=1` | 404, 409 already revoked, 501 |
| `GET /keys/audit?since=` | admin | audit fragment | 200 | 400 bad `since`, 501 |

`since` accepts the presets `1h`, `24h`, `7d`, `all` (default `24h`). The handler
converts it to an instant and applies the same retention floor and limit (100)
as `/api/auth/audit` using `Config.AuditRetention()`; anything else is 400. The
build should factor that logic out of `handleListAudit` into a shared helper
rather than duplicate it. All routes set `Cache-Control: no-store` (reuse
`keyStore(w)` semantics, adapted to render HTML for 501). Form bodies are capped
with `MaxBytesReader` at the existing 4 KiB limit.

## 4. Page layout

Nav: a fourth entry "API keys" (`/keys`, `Nav: "keys"`) in `base.html`, rendered
only when a new `pageData.CanAdmin` is true (principal scope admin, or auth
disabled). Hiding is cosmetic; the route enforces. A non-admin who types the URL
gets the existing JSON 403 (accepted; see open questions).

`GET /keys` has three `panel` sections under the topbar title "API keys":

1. **Create key** form: `Name` (text, required, `maxlength` = `MaxKeyNameLen`),
   `Scope` (`<select>` eval/read/write/admin, options above the caller's scope
   omitted since the ceiling would reject them; default `read`), `Expires`
   (`<input type=date>`, optional; end of that day UTC; must be in the future).
   Submit "Create key" (`btn btn-primary`). Hint: "The key is shown once."
2. **Keys** table: Name, Scope, Created, Expires, Last used, Status, and an
   action column with a "Revoke" `btn` (omitted for non-active keys). Times are
   `<time datetime="RFC3339">` shown as `2006-01-02 15:04 UTC`; no expiry shows
   "Never expires" in `muted`, no use shows "Never". Status is a text label
   (Active / Expired / Revoked) with a leading `.dot` (`operational` / `degraded`
   / `down`); meaning never relies on colour alone. Order: active first, then
   created descending. A note states that env-defined tokens are not listed.
3. **Audit log** with presets and the retention line (section 7).

## 5. Create, one-time secret, revoke

**Create success** (201, `no-store`) re-renders the page with a status banner
above the form (`role="status"`, heading with `tabindex="-1"` and focus moved
there by a plain `autofocus`) containing the key name and scope and
`<label for=secret>New key (shown once)</label>` with
`<input id=secret class="mono" readonly autocomplete="off" value=TOKEN>` styled
`user-select: all`, so one click or tap selects the whole token with no script.
Hint: "Copy it now. It cannot be shown again." The list below comes from the
store and contains no token. Reload or revisiting issues a GET; no URL returns
the secret.

**Copy button (optional enhancement).** A static `/static/keys.js` (allowed by
`script-src 'self'`, `defer`, loaded only on the create-success page) un-hides a
`<button hidden>Copy</button>` and calls `navigator.clipboard.writeText`, then
updates a `role="status"` span ("Copied"). It must not log, store or transmit
the value. Select-on-click is the baseline and the page works without the
script. If the reviewer prefers zero JS, drop the file.

**Validation errors** re-render the page with the status from section 3, form
values retained (never a token), an error summary `<p class="login-error"
role="alert">`, and `aria-invalid="true"` plus `aria-describedby` on the field.
Messages reuse the engine's `ErrInvalidKey` text; 409 "A key with that name
already exists"; 403 "You cannot create a key with a higher scope than your own".

**Revoke is two steps on one route.** The row button is a form posting
`/keys/{id}/revoke` with no `confirm` field. Step 1 changes nothing and returns
a 200 confirmation page (not `hx-confirm`, for testability): "Revoke key NAME?
Anything using it stops working immediately." with `Cancel` (link to `/keys`)
and `Revoke key` (form posting `confirm=1` and the CSRF token, `btn-primary`).
Focus starts on the heading; Tab order is Cancel, then Revoke. Step 2
(`confirm=1`) calls `RevokeManagedKey` and 303s to `/keys?revoked=1`, which
shows a `role="status"` note "Key revoked." (static text; the name is not
echoed from the query).

**Self-revoke.** The server compares `Principal.KeyID` to `{id}` itself on both
steps; the form never carries a trusted "self" flag. When equal, step 1 adds a
`role="alert"` warning (`login-error` style): "This is the key behind your
current session. Revoking it signs you out immediately." and the button reads
"Revoke and sign out". There is no force option. On step 2 success the handler
destroys the session and clears the cookie (as `handleLogout` does) and 303s to
`/login?signed_out=1&key_revoked=1`; login shows "Your key was revoked, so you
were signed out." (small addition to `login.html`/`loginData`). The Chain digest
re-check (#101) stays the authority: even without that redirect, the next
request is 303 to `/login`. Revoking any other key leaves the session intact.

## 6. States

| State | Behavior |
|---|---|
| Empty list | row "No managed keys yet. Create one above." (`muted`) |
| Empty audit | "No key events in this period." |
| Store error (500) | generic message plus request ID; no internals |
| 501 no KeyStore | status 501 page: managed keys need the SQLite store; env tokens still work; no forms |
| Permission denied | existing 403 / 303; nav link hidden for non-admins |
| Session ended after self-revoke | `/login` note above; unauthenticated htmx audit GET already answers `HX-Redirect: /login` |
| Already revoked | 409 page "That key is already revoked." with link back |
| Not found | 404 page with link back |
| Audit write failed | action stands; notice "Done, but the audit event could not be recorded"; error logged (same policy as #104) |
| Auth disabled | pages work; no CSRF field; audit principal `anonymous`; self-revoke impossible |

## 7. Audit view

Columns: Time (UTC), Principal, Action (`key.create` / `key.revoke`), Key (name
when resolvable from the key list, else the id, `mono`), Request ID (`mono`).
Presets "1 hour / 24 hours / 7 days / All retained" are real links
(`/keys?since=24h`) that work without htmx; with htmx they add
`hx-get="/keys/audit?since=24h" hx-target="#audit" hx-swap="innerHTML"
hx-push-url="false"`. The current preset has `aria-current="true"` and bold
text. `#audit` is `aria-live="polite"`. Below the controls: "Audit events are
kept for 90 days." or "Audit events are kept forever." from
`Config.AuditRetention()`, and "Showing the latest 100 events." when the limit
is reached. Rows never contain a token or digest.

## 8. Accessibility (WCAG 2.1 AA)

- Every control has a visible `<label for>`; hints via `aria-describedby`.
- Landmarks from `base.html`; one `h1` (topbar), `h2` per panel. Tables use
  `<th scope="col">` and an `aria-label`. Row buttons carry
  `aria-label="Revoke key NAME"`.
- Focus order follows the DOM: nav, create form, list, audit. Extend the
  existing `:focus-visible` rule to `select` and `input`. After create or
  revoke the status banner takes focus; errors use `role="alert"`, success
  `role="status"`.
- Fully keyboard operable, no hover-only affordances, no custom widgets, no JS
  required for any action.
- Contrast with existing tokens: `--c-ink`/`--c-ink-2` on `--c-surface`;
  `--c-muted` on white (about 4.8:1) but not on `--c-canvas-2` (about 4.2:1);
  `--c-danger` on white for errors; white on `--c-primary`. Do not use
  `--c-ok`, `--c-faint` or `--c-discover` as text colour (they fail 4.5:1). The
  existing `.login-note` uses `--c-ok`; the new success notes use `--c-ink`
  with a left border in `--c-anchor` instead. The status dot is decorative.

## 9. Security notes

- Secret: only in the 201 body, `no-store`, never logged (access log records
  route and status only; form bodies are not logged), `autocomplete="off"`.
  Keys pages add the `Referrer-Policy: no-referrer` meta that login uses.
- Output is escaped by `html/template`; key names are user-controlled, so no
  `template.HTML` on these pages.
- Audit: every create and revoke writes a persisted event (session principal,
  request ID). CSRF failures are already counted and logged.
- Privacy: no new PII (key names, principal labels). Retention and erasure
  follow #102 (90 days default, daily prune).
- Rollback: additive (routes, templates, one nav link, one login note); revert
  the PR. No migration.

## 10. Test plan

`internal/server/keysui_test.go` (httptest, table-driven, real SQLite KeyStore
via existing helpers):

- Scope matrix for all four routes: anonymous 303 `/login`; read/write session
  403; admin 200; `routeScopes` coverage test passes.
- CSRF: POST `/keys` and `/keys/{id}/revoke` with missing token, wrong token and
  cross-origin (`Sec-Fetch-Site: cross-site`) each 403 with no key created or
  revoked.
- Create: 201 body contains the token exactly once; `Cache-Control: no-store`,
  `HX-Push-Url: false`, `hx-history="false"` present; the token authenticates
  through the Chain at the chosen scope; a following `GET /keys` contains
  neither token nor digest; a `key.create` event carries the session principal
  and request ID. Errors: empty or overlong name 400, bad scope 400, past
  expiry 400, scope above ceiling 403, duplicate 409, oversized body 413; form
  values retained; a name containing `<script>` is escaped.
- Revoke: step 1 shows the confirm page and revokes nothing; step 2 revokes and
  audits `key.revoke`; unknown id 404; repeat 409. Self-revoke: warning and
  "Revoke and sign out" shown, step 2 redirects with `key_revoked=1`, the old
  cookie then gets 303, session destroyed; revoking another key keeps the
  session; an env-token session (empty `KeyID`) never sees the warning.
- Audit: each preset filters correctly, unknown `since` 400, retention floor
  applied, retention line for 90 days and for 0 (forever), limit note, htmx
  request returns only the fragment, unauthenticated htmx GET returns
  `HX-Redirect`.
- States: 501 on all routes with no KeyStore and no forms rendered; empty list
  and audit text; store failure gives 500 without internals; audit write
  failure shows the notice.
- CSP: header unchanged, and rendered HTML contains no inline `<script>`, `on*=`
  or `style=`.
- A11y smoke (string-level): each `input`/`select` has a matching `label for`;
  `role` attributes present; `th scope`. Manual checklist in the PR: keyboard
  only pass, screen-reader announcement of status, 200% zoom, contrast spot
  check.

## 11. Size

One PR is feasible: handlers (about 250 lines), two templates, about 40 lines
of CSS, optional `keys.js`, tests. If review finds it too large, split into
(a) keys page (create, list, revoke, self-revoke) then (b) audit fragment, with
operator sign-off per the grooming.

## 12. Open questions (resolved)

1. A non-admin typing `/keys` keeps the existing JSON 403. A styled HTML 403 is
   a cross-cutting change; deferred.
2. `keys.js` is included (the existing `script-src 'self'` allows it; the CSP is
   unchanged). Select-on-click remains the baseline; the page works without it.

## 13. Implementation notes (deviations and details)

- The audit retention floor moved into `Server.floorAudit`, shared by
  `/api/auth/audit` and the dashboard view. The default limit constant is shared
  too.
- The 4 KiB form cap: the auth middleware already parses the form for the CSRF
  check (Go bounds that at 10 MB), so `MaxBytesReader` alone cannot enforce the
  cap on cookie sessions. The handlers also check `Content-Length` and the total
  decoded field size and answer 413 beyond 4 KiB.
- Revoke with a failed audit write answers a 200 page with the notice instead of
  the redirect (the redirect would lose the notice). Self-revoke still redirects
  to `/login`; the audit failure is only logged there.
- The audit fragment route answers a small HTML alert (400) for a bad `since`,
  and the full 501 page when there is no KeyStore.
- If the key list cannot be loaded right after a successful create, the page
  still shows the token (it exists nowhere else) with a list-unavailable alert.
- `hx-history="false"` is set on `<main>` via `pageData.NoHistory`.
