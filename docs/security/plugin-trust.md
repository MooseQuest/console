# Plugin trust & isolation

Console's out-of-process plugin architecture (described in
[plugins-architecture.md](../plugins-architecture.md)) trades in-process
simplicity for isolation. That isolation comes with a specific trust model that
operators and plugin authors need to understand.

Related documents:
- [Supply-chain security SOP](supply-chain.md) — dependency policy,
  `govulncheck`, Dependabot, pinned Actions; plugin distribution as a
  supply-chain input.
- [Runtime hardening](runtime-hardening.md) — API auth, secrets, SSRF,
  DoS controls.

---

## Threat model

### What the plugin architecture does

The `console` host process launches `console-plugin-*` executables as
subprocesses and talks to them over a local gRPC channel (managed by
[hashicorp/go-plugin](https://github.com/hashicorp/go-plugin)). The plugin
process runs for the lifetime of the host and handles requests from the host's
engines.

### What it does NOT do (and what that means for trust)

**Plugin processes run with host privileges.** The host and plugin share the
same Unix user; the plugin can read the host's file descriptors, procfs
information, and — critically — its environment variables.

**Plugins inherit the host's full environment by default.** Every secret the
host knows about — `ANTHROPIC_API_KEY`, `CONSOLE_DB`, `HEROKU_API_KEY`, every
webhook URL and SMTP password — is present in the plugin's environment unless
the host is explicitly configured to pass a restricted environment. This is how
plugins read their own credentials without extra wiring, and it is also the
threat: a malicious or compromised plugin binary can exfiltrate all host
credentials.

**There is no OS-level sandbox.** The plugin is a sibling process, not a
container or VM. Syscall filtering (seccomp, pledge) is not applied. The
plugin's blast radius is the same as the host's.

This means: **running a plugin binary is equivalent in trust to running the
host binary itself.** Treat any plugin binary with the same scrutiny you apply
to the host.

### The gRPC channel

The host and plugin communicate over a local TCP or Unix-socket connection
negotiated by go-plugin's handshake. The channel is authenticated via mutual
TLS (AutoMTLS), which go-plugin generates on each startup.

- ✅ **AutoMTLS is enabled.** The handshake exchanges ephemeral certificates so
  that only the process the host launched can connect back. A local process
  attempting to hijack an established plugin connection would fail the mTLS
  handshake.
- The channel is local-only; it is not exposed on external interfaces.

### Attack surface summary

| Vector | Current state | Notes |
|---|---|---|
| Malicious plugin binary | **Operator responsibility** | Binary runs with host privileges and reads host env. Only run plugins from trusted sources. |
| Plugin binary path tampering | 🔜 Planned (checksum verification) | See SecureConfig section below. |
| Host-to-plugin channel hijack | ✅ Mitigated (AutoMTLS) | Ephemeral mTLS per process launch. |
| Plugin credential over-exposure | 🔜 Planned (minimal env) | Today all host env vars reach the plugin. |
| Third-party plugin supply chain | **Operator responsibility** | Verify provenance and checksum. |

---

## Standards and controls

### AutoMTLS — ✅ Done (v0.2.1)

go-plugin's `AutoMTLS` is enabled in the host's plugin client configuration.
On each launch, go-plugin generates a fresh ephemeral CA and issues a client
certificate to the host and a server certificate to the plugin, exchanged over
the handshake. Only the process the host actually launched holds the right
private key; a rogue local process cannot impersonate the plugin.

Do not disable `AutoMTLS` in any plugin or host code. If you find it disabled
in a plugin's `plugin.ServeConfig`, treat that as a security defect and
report it.

### Absolute, explicitly-configured plugin paths

Plugin paths come from environment variables (`CONSOLE_STORE_PLUGIN`,
`CONSOLE_STATUS_PLUGINS`, `CONSOLE_NOTIFY_PLUGINS`, `CONSOLE_LLM_PLUGIN`).
The host does not search `$PATH` or any relative directory for plugin binaries.

Operators **must** supply absolute paths. Relative paths or `$PATH`-resolved
names are not supported and will be rejected at startup. This prevents an
attacker who can write a file named `console-plugin-postgres` into `$PATH`
from hijacking a plugin slot.

### Checksum verification via SecureConfig — 🔜 Planned

go-plugin's `SecureConfig` allows the host to verify a SHA-256 (or stronger)
checksum of the plugin binary before launching it. If the binary does not match
the expected hash, the host refuses to start it.

The planned workflow:

1. Each official release publishes a `SHA256SUMS.txt` file alongside the plugin
   binaries (same file used by the supply-chain SOP).
2. Operators configure the expected checksum for each plugin in their
   environment or a config file.
3. The host reads the checksum and passes it to go-plugin's `SecureConfig`.
4. A binary that has been tampered with or replaced fails the hash check at
   startup with a clear error, rather than running silently.

Until SecureConfig is wired in, operators who require strong integrity
guarantees should verify plugin binaries manually against `SHA256SUMS.txt` before
deployment, and run them from a directory with restricted write permissions
(e.g., owned by root, not writable by the service account).

### Minimal environment passed to plugins — 🔜 Planned

Today the host passes `os.Environ()` to each plugin subprocess. The planned
change is to build a minimal, explicitly-enumerated env that contains only the
variables the plugin is known to need, using go-plugin's `Cmd.Env` field.

This limits credential blast radius: a compromised `console-plugin-slack`
binary would only see `CONSOLE_SLACK_WEBHOOK_URL`, not
`ANTHROPIC_API_KEY` or `CONSOLE_DB`.

The per-plugin env map will be derived from the documented "Reads" column in
[plugins-architecture.md](../plugins-architecture.md). Each plugin seam will
declare the env vars it requires; the host assembles the env at launch time.

---

## Operator guidance

### Golden rule

**Only run plugin binaries that you compiled yourself from source you
reviewed, or that you obtained from a source you trust and verified against a
published checksum.** There is no sandbox. A plugin binary you run has the
same access to your credentials as the host process.

### Operator checklist

- [ ] Plugin binaries are stored in a directory owned by the service account
  or root, with write permissions restricted so that non-root users cannot
  replace them.
- [ ] Plugin paths are set to absolute paths (e.g.,
  `/opt/console/bin/console-plugin-postgres`, not `./bin/...`).
- [ ] If the binary came from a release archive, the SHA-256 checksum has been
  verified against the published `SHA256SUMS.txt` file before first run.
- [ ] If the binary was built locally, it was built from a tagged release
  commit, not from an unreviewed branch.
- [ ] The service account running `console` does not have unnecessary
  filesystem permissions (principle of least privilege).
- [ ] Secrets passed to Console via environment variables are scoped to the
  minimum set of plugins that need them. (Full isolation is planned; scope
  reduction is actionable today.)

### Upgrading plugins

When upgrading a plugin binary:

1. Download the new binary and verify its checksum against `SHA256SUMS.txt` from
   the same release.
2. Replace the binary in the restricted directory.
3. Restart `console` — the host relaunches all plugin subprocesses on startup.

Do not hot-swap a plugin binary while `console` is running; the host holds a
reference to the launched process and will not notice the replacement until
restart.

---

## Contributor guidance (writing a new plugin)

If you are writing a new `console-plugin-*`, follow these rules:

### Required

- **Call the seam's `Serve` helper.** Do not write your own go-plugin
  `ServeConfig`. The `Serve` helper in `internal/plugin` sets `AutoMTLS: true`
  and other security defaults. Overriding it to disable `AutoMTLS` is a
  security defect.
- **Read only the env vars you need.** Document them in the "Reads" column of
  [plugins-architecture.md](../plugins-architecture.md). When the minimal-env
  campaign lands, this documentation is what determines what the plugin
  receives.
- **Do not write credentials to disk.** If your plugin needs to cache a token
  or derived credential, keep it in memory only.
- **Do not log credential values.** Log the presence of a credential ("`API
  key configured`") but never its value, even in debug output.
- **Return errors as gRPC status codes.** Map `core.ErrNotFound` to
  `codes.NotFound`, `core.ErrConflict` to `codes.AlreadyExists`, etc. The host
  maps these back to typed errors; arbitrary error strings may leak internal
  state.

### Recommended

- **Bound all outbound HTTP calls.** Use a context with a timeout derived from
  the one passed to your `Check` or `Complete` call. Do not make unbounded
  network calls. See the SSRF guidance in [runtime-hardening.md](runtime-hardening.md).
- **Validate config at startup.** If a required env var is missing, log a
  clear error and exit. Do not silently succeed and fail at call time — that
  makes incidents harder to diagnose.
- **Provide a `Name()` that is stable across versions.** The host routes
  component checks by provider name. Changing `Name()` breaks existing
  component configurations.

### PR checklist for a new plugin

- [ ] `Serve` helper used, `AutoMTLS` not overridden.
- [ ] Required and optional env vars documented in the "Reads" column of
  `docs/plugins-architecture.md`.
- [ ] No credentials written to disk or logged.
- [ ] Outbound HTTP calls are bounded by context timeout.
- [ ] `govulncheck ./...` clean (no new reachable vulnerabilities introduced).
- [ ] License of any new dependency reviewed (see
  [supply-chain.md](supply-chain.md)).

---

## Future: plugin signing and allowlist

A longer-term control is a signed plugin registry: official plugins are signed
with a Console release key, and the host refuses to launch a binary whose
signature does not match an entry in an operator-configurable allowlist. This
is analogous to Terraform's provider signing model.

This is not scheduled for a specific release. It will be designed in a
separate document when the checksum verification (SecureConfig) and minimal-env
campaigns are complete. Surface any design feedback on the parent tracking
issue.
