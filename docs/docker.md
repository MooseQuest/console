# Running Console in Docker

Console ships as a small, multi-arch container image (`linux/amd64` and
`linux/arm64`) built from the single static binary on a distroless base — about
20 MB, and it starts in milliseconds.

## Quick start

The container binds `0.0.0.0:8080` (so `-p` works), and Console refuses to start
on a non-loopback address without an API token. Generate one, then pass it in:

```bash
export CONSOLE_ADMIN_TOKENS=$(docker run --rm ghcr.io/moosequest/console:latest auth token)
docker run --rm -p 8080:8080 -e CONSOLE_ADMIN_TOKENS ghcr.io/moosequest/console:latest
```

Open <http://localhost:8080> and sign in with the token (keep it; it is not
stored anywhere else). That's the dashboard + API, backed by the built-in SQLite
store and `http` status provider. Without a token the container exits at startup
with a `refusing to start` error.

The same image is published to Docker Hub as `moosequest/console:latest`.

## Persisting data

The container stores its SQLite database at `/data/console.db` (declared a
volume). Without a mount, `--rm` throws the data away when the container stops.
Mount a volume to keep it:

```bash
docker run -d --name console \
  -p 8080:8080 \
  -v console-data:/data \
  -e CONSOLE_ADMIN_TOKENS \
  ghcr.io/moosequest/console:latest
```

Or with Compose (see [`docker-compose.yml`](../docker-compose.yml) at the repo
root), which reads the token from your shell or an `.env` file:

```bash
export CONSOLE_ADMIN_TOKENS=$(docker run --rm ghcr.io/moosequest/console:latest auth token)
docker compose up -d
```

For anything beyond a laptop, use a file-based secret instead of an environment
variable, so the token does not appear in `docker inspect`:

```bash
docker run --rm ghcr.io/moosequest/console:latest auth token > console-admin-token
docker run -d -p 8080:8080 -v console-data:/data \
  -v "$PWD/console-admin-token:/run/secrets/console-admin-token:ro" \
  -e CONSOLE_ADMIN_TOKENS_FILE=/run/secrets/console-admin-token \
  ghcr.io/moosequest/console:latest
```

The container runs as uid 65532, so the token file must be readable by it.

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
  -e CONSOLE_ADMIN_TOKENS \
  -e CONSOLE_SLACK_WEBHOOK_URL="https://hooks.slack.com/services/..." \
  ghcr.io/moosequest/console:latest
```

> **Note.** This image is `console` only — it does not bundle the
> `console-plugin-*` binaries. The built-in SQLite store and `http` status
> provider work out of the box; out-of-process plugins are a separate concern
> (see [plugins-architecture.md](plugins-architecture.md)).

## Health checks

Probe `GET /healthz`: it is unauthenticated and returns `200 ok`. Do not probe
`/api/health`, which needs a `read` token and returns `401` without one.

The image is distroless (no shell, no `curl`, no `wget`), so a Dockerfile
`HEALTHCHECK` cannot shell out, and the `console` binary has no probe
subcommand yet. Run the probe from outside the container instead, for example
in Kubernetes:

```yaml
livenessProbe:
  httpGet: { path: /healthz, port: 8080 }
readinessProbe:
  httpGet: { path: /healthz, port: 8080 }
```

or from your load balancer or uptime monitor.

## Security

Console authenticates API and dashboard requests with scoped bearer tokens. In
the container, `CONSOLE_ADDR` is `0.0.0.0:8080`, so a token is mandatory, and
the container does not terminate TLS. Put it behind a TLS-terminating proxy
(set `CONSOLE_COOKIE_SECURE=always` there) or publish only to localhost
(`-p 127.0.0.1:8080:8080`). Behind a proxy, all clients share one login-throttle
key; see [security/runtime-hardening.md](security/runtime-hardening.md) for that
caveat, token scopes, rotation, and the setting for authenticating-proxy
setups.

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
