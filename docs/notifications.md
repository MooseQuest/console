# Notifications

Console doesn't just observe — it can tell you when something changes. The flag
and status engines emit **events**, and a **dispatcher** fans each event out to
every registered **notifier** (Slack today; webhook and email are natural
additions behind the same interface).

- [Events](#events)
- [The Slack notifier](#the-slack-notifier)
- [How emission works](#how-emission-works)
- [Writing a notifier](#writing-a-notifier)

## Events

An event (`core.Event`) is a flat, self-describing record so any sink can render
it without extra lookups:

| Type | Emitted when |
|---|---|
| `component_down` | a component transitions to **down** |
| `component_degraded` | a component transitions to **degraded** |
| `component_recovered` | a component returns to **operational** from down/degraded |
| `flag_changed` | a flag is created, updated, or deleted |

Status transitions are computed by comparing each new check against the last
recorded state. A **first-ever** check is treated as a transition from
`unknown`, so a newly added broken component still alerts — but a healthy first
check (`unknown → operational`) stays quiet, and a steady state never re-alerts.

Each event carries a `Severity()` hint (`critical` / `warning` / `good` /
`info`) that notifiers use for things like message color.

## The Slack notifier

The built-in Slack notifier posts to an **Incoming Webhook** — no bot token or
OAuth scopes, just the URL. Enable it by setting:

```bash
export CONSOLE_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/T.../B.../xxxx
console serve
```

Each event becomes a colored Slack attachment (red for down, amber for degraded,
green for recovered, indigo for flag changes) titled with the component/flag and
a short message. When no webhook is configured, no notifier is registered and
the engines skip emission entirely (including the extra read it would cost).

## How emission works

```
status.Engine.Run ──┐
                    ├─ emit(core.Event) ─▶ notify.Dispatcher.Emit ─▶ each Notifier
flags.Engine.{Create,Update,Delete} ─┘
```

The dispatcher is **best-effort**: each notifier call is bounded by a timeout
against a fresh background context (so a notification outlives the request that
triggered it), and a slow or failing sink is logged — it never fails the
operation that produced the event. The engines receive the dispatcher's `Emit`
via `SetEmitter`, wired in the app composition root only when at least one sink
is configured.

## Writing a notifier

Implement `notify.Notifier` and register it on the dispatcher in
`internal/app/app.go`:

```go
type Notifier interface {
    Name() string
    Notify(ctx context.Context, ev core.Event) error
}
```

A webhook notifier, for example, would `POST` the event as JSON; an email
notifier would format it as a message body. See
[plugins](plugins.md) for the full plugin-seam overview.
