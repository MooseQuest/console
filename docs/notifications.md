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

## The Slack notifier plugin

Slack ships as an **out-of-process plugin** (`console-plugin-slack`) — see
[plugin architecture](plugins-architecture.md). It posts to a Slack **Incoming
Webhook** (no bot token or OAuth scopes, just the URL). Point the host at the
plugin and give the plugin its webhook via the environment it inherits:

```bash
make build && make plugins        # -> ./bin/console-plugin-slack
export CONSOLE_NOTIFY_PLUGINS=$PWD/bin/console-plugin-slack
export CONSOLE_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/T.../B.../xxxx
./console serve
```

Each event becomes a colored Slack attachment (red for down, amber for degraded,
green for recovered, indigo for flag changes) titled with the component/flag and
a short message. When no notifier plugin is configured, no sink is registered and
the engines skip emission entirely (including the extra read it would cost).

> Slack isn't the only sink. Two more notifiers ship as plugins:
> `console-plugin-webhook` (POSTs each event as JSON, with an optional
> `X-Webhook-Secret`) and `console-plugin-email` (SMTP). List the ones you want
> in `CONSOLE_NOTIFY_PLUGINS` — they run side by side. See
> [plugin architecture](plugins-architecture.md) for the full catalog and config.

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

Implement `notify.Notifier`, then serve it as an out-of-process plugin (the
Slack plugin is the template — `cmd/console-plugin-slack` + the notifier adapters
in `internal/plugin`):

```go
type Notifier interface {
    Name() string
    Notify(ctx context.Context, ev core.Event) error
}
```

A webhook notifier, for example, would `POST` the event as JSON; an email
notifier would format it as a message body. Each is built into its own
`console-plugin-<name>` binary and listed in `CONSOLE_NOTIFY_PLUGINS`. See
[plugin architecture](plugins-architecture.md) for the full design.
