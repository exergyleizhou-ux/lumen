# Bash command sandbox (optional, off by default)

Lumen's `bash` tool normally runs `sh -c <command>` directly on the host with
the user's full privileges and network. The `internal/sandbox` command runner
lets you optionally wrap each `bash` command in an OS-level sandbox so a
denylist miss (see [threat-model.md](threat-model.md) §6) is *contained* rather
than catastrophic.

This is **defense in depth**, not a replacement for the guard. It is **off by
default** — nothing changes unless you opt in.

## Enabling it

| Env var | Values | Effect |
|---|---|---|
| `LUMEN_BASH_SANDBOX` | unset / `0` / `false` / `off` | **Off (default).** `bash` runs directly — historical behavior. |
| | `1` / `true` / `on` / `yes` / `required` | **Required.** Every `bash` command runs in the sandbox. If no backend is available on this platform, `bash` **fails closed** (errors instead of running unsandboxed). |
| | `auto` | Use the sandbox if a backend is available, otherwise run directly. |
| `LUMEN_BASH_SANDBOX_NET` | unset / `0` | **Network denied** inside the sandbox (default when the sandbox is on). |
| | `1` / `true` / `on` / `yes` | Allow network inside the sandbox. |

```sh
# Contain bash: no network, writes confined to the workspace + temp.
export LUMEN_BASH_SANDBOX=1
lumen run "..."

# Allow network (e.g. for `go mod download`, `git pull`) but keep write confinement.
export LUMEN_BASH_SANDBOX=1 LUMEN_BASH_SANDBOX_NET=1
```

## Backends

| OS | Backend | Requires |
|---|---|---|
| macOS | `sandbox-exec` (Seatbelt) | ships with macOS |
| Linux | `bwrap` (bubblewrap) | `bubblewrap` package installed |

`SelectRunner()` picks the platform-appropriate backend, or returns nil when
none is available.

## What it contains (and what it does not)

**Contained:**
- **Network** — denied by default. This is the single most valuable control: it
  defeats exfiltration (`curl … -d @secret`) and download-and-execute
  (`curl … | sh`) regardless of whether the guard's denylist spotted them.
- **Writes** — confined to the working directory and the system temp dirs
  (`/tmp`, the OS `TMPDIR`). A command cannot write `~/.ssh/authorized_keys`,
  shell rc files, git hooks, or `/etc` — the persistence vectors in the threat
  model.

**Deliberately NOT contained:**
- **Reads** stay allowed. Coding needs to read the tree and system libraries,
  and reads don't mutate; the guard already blocks the worst sensitive reads
  (`/etc/shadow`, `.env`, `id_rsa`). A future tightening could confine reads too.
- **Temp dirs are writable** because real tools need `TMPDIR`. So the guarantee
  is "writes confined to workspace **+ temp**", not "workspace only".
- This is **not** a security boundary against a kernel exploit or a sandbox
  escape; it is a capability reduction that shrinks the blast radius of a
  prompt-injected or mistaken command.

## Status

The runner and the `bash` opt-in are implemented and tested (the macOS Seatbelt
backend has a real containment integration test). The default is off pending
broader real-world validation; once proven, enabling it (or surfacing it in the
typed config / `lumen doctor`) is a follow-up owned by the config/system track.
