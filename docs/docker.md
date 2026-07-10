# Running Console in Docker

Console ships as a small, multi-arch container image (`linux/amd64` and
`linux/arm64`) built from the single static binary on a distroless base — about
20 MB, and it starts in milliseconds.

## Quick start

```bash
docker run --rm -p 8080:8080 ghcr.io/moosequest/console:latest
```

Open <http://localhost:8080>. That's the dashboard + API, backed by the built-in
SQLite store and `http` status provider — no configuration required.

The same image is published to Docker Hub:

```bash
docker run --rm -p 8080:8080 moosequest/console:latest
```

## Persisting data

The container stores its SQLite database at `/data/console.db` (declared a
volume). Without a mount, `--rm` throws the data away when the container stops.
Mount a volume to keep it:

```bash
docker run -d --name console \
  -p 8080:8080 \
  -v console-data:/data \
  ghcr.io/moosequest/console:latest
```

Or with Compose (see [`docker-compose.yml`](../docker-compose.yml) at the repo
root):

```bash
docker compose up -d
```

## Configuration

The image reads the same `CONSOLE_*` environment variables as the CLI. The
container sets two defaults that differ from a bare `console serve`:

| Variable | Container default | Why |
|---|---|---|
| `CONSOLE_ADDR` | `0.0.0.0:8080` | A loopback bind would be unreachable through `-p`; the container's network namespace is the boundary. |
| `CONSOLE_DB` | `/data/console.db` | Lives on the mounted volume. |

Pass any other variable through with `-e`, e.g. a notifier plugin:

```bash
docker run -p 8080:8080 \
  -e CONSOLE_SLACK_WEBHOOK_URL="https://hooks.slack.com/services/..." \
  ghcr.io/moosequest/console:latest
```

> **Note.** This image is `console` only — it does not bundle the
> `console-plugin-*` binaries. The built-in SQLite store and `http` status
> provider work out of the box; out-of-process plugins are a separate concern
> (see [plugins-architecture.md](plugins-architecture.md)).

## Security

Console still has **no built-in authentication**. Inside the container it binds
`0.0.0.0:8080` so `-p` works, but that does **not** make it safe to expose on an
untrusted network. Publish the port only to `localhost` (`-p 127.0.0.1:8080:8080`)
or place an authenticating reverse proxy in front of it. See
[security/runtime-hardening.md](security/runtime-hardening.md).

The image runs as a **nonroot** user (uid 65532) on a distroless base with no
shell or package manager, which keeps the attack surface minimal.

## Tags

| Tag | Meaning |
|---|---|
| `latest` | The most recent published release. |
| `X.Y.Z` (e.g. `0.5.0`) | A specific release. |
| `X.Y` (e.g. `0.5`) | The latest patch of that minor line. |
| `sha-<short>` | The exact commit an image was built from. |

Pin to `X.Y.Z` for reproducible deployments.
