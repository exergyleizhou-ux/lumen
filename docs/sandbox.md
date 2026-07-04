# Bash command sandbox (auto by default)

Lumen's `bash` tool normally runs `sh -c <command>` on the host. The
`internal/sandbox` command runner can wrap each `bash` command in OS-level
isolation so a denylist miss (see [threat-model.md](threat-model.md) §6) is
*contained* rather than catastrophic.

**Default is `auto`:** when `sandbox-exec` (macOS) or `bwrap` (Linux) is
available, `bash` uses it automatically. Set `LUMEN_BASH_SANDBOX=off` to restore
direct host execution.

## Modes

| Env var | Values | Effect |
|---|---|---|
| `LUMEN_BASH_SANDBOX` | unset / `auto` | **Auto (default).** Use sandbox when a backend exists; otherwise run directly. |
| | `0` / `false` / `off` / `disabled` | **Off.** Direct `sh -c` — no isolation. |
| | `1` / `true` / `on` / `yes` / `required` | **Required.** Fail closed if no backend is available. |
| `LUMEN_BASH_SANDBOX_NET` | unset / `0` | **Network denied** inside the sandbox. |
| | `1` / `true` / `on` / `yes` | Allow network inside the sandbox. |

```sh
# Default auto — sandbox when backend exists.
lumen run "..."

# Force off (legacy direct behavior).
export LUMEN_BASH_SANDBOX=off

# Require sandbox — errors instead of running unsandboxed when unavailable.
export LUMEN_BASH_SANDBOX=required
```

## Backends

| OS | Backend | Requires |
|---|---|---|
| macOS | `sandbox-exec` (Seatbelt) | ships with macOS |
| Linux | `bwrap` (bubblewrap) | `bubblewrap` package installed |

## What it contains

- **Network** — denied by default.
- **Writes** — confined to working directory + system temp dirs.

Reads stay allowed (coding needs tree access). This is blast-radius reduction,
not a kernel-level security boundary.