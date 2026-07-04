# Lumen Science Bridge — Hardening Notes

> Engineering patterns adopted into Lumen Science (2026-07).

## High-impact ports

1. **first_http_url** — sandbox `url` output: only the first valid `http(s)` line; ignore notes/extra lines.
2. **login_intact self-heal** — health checks login integrity first; broken virtual OAuth triggers repair, preserves org stickiness.
3. **CONNECT fast-fail 401** — targeted fast-fail on `anthropic`/`claude.*` hosts → 401 (not 403) for operon logged-out detection.
4. **Extreme atomic config write** — pid+seq `O_EXCL` tmp, `Sync`, rename, `0600` reset.
5. **Multi-profile transactional switch** — upstream `/v1/messages` probe before commit; rollback on proxy restart failure.
6. **DSML tool-use shim** — `off` / `detect` / `rewrite` for DeepSeek wire-format leaks.
7. **Legacy config import** — `lumen science migrate` imports prior bridge settings when present.

## Lumen locations

| Pattern | Location |
|---------|----------|
| first_http_url | `internal/science/launcher/launcher.go` |
| login_intact | `internal/science/oauth/forge.go`, `launcher.Running` |
| CONNECT 401 | `internal/science/proxy/proxy.go` |
| Atomic config | `internal/science/config/config.go` |
| Profile switch | `internal/science/runtime/profile_switch.go` |
| DSML shim | `internal/science/proxy/dsml*.go` |
| Legacy import | `internal/science/migrate/legacy_bridge.go`, `config/profiles.go` |

## Lumen-only strengths (retained)

- 5-ship native MCP fleet, Research Brief 4-source, Oasis OAuth/C2D embed
- Go single-binary agent + science stack
- `make goal-all-verify` / RM offline automation gates