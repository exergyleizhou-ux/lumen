# Audit trail (tool-call provenance)

`internal/audit` records every agent tool call — the tool, the model's stated
reason (*why*), the arguments, and the result — to an append-only, hash-chained
JSONL log. It answers the forensic question the threat model raises
([threat-model.md](threat-model.md) §7, G5): *"why did the agent run this?"*,
and it survives restarts.

## Storage

| Env var | Default | Effect |
|---|---|---|
| `LUMEN_AUDIT` | on | Set `off`/`0`/`false`/`none` to disable persistence entirely (no file written; `Record` becomes a no-op). |
| `LUMEN_AUDIT_LOG` | `~/.lumen/audit.jsonl` | Override the JSONL path. |

Each line is one `audit.Entry` with a `hash`/`prev_hash` chain, so tampering
(editing or deleting a middle line) is detectable via `Store.Verify()`. Args and
result are truncated at 4 KiB to keep the log bounded.

## Recording (the one-line hook for the agent loop)

The agent loop (owned by the system/orchestrator track) records a call in one
line per tool execution:

```go
audit.Record(audit.ToolCall{
    Tool:   name,            // e.g. "bash"
    Why:    reason,          // the model's stated reason, if available
    Args:   string(args),    // tool arguments (JSON)
    Result: summary,         // short result or error
    OK:     err == nil,
})
```

`audit.Record` is nil-safe and a no-op when auditing is disabled, so the call
site needs no guard. `Session` may be set to correlate entries to a run.

> Status: the store, the `Record` API, and the `audit_query` reader are
> implemented and tested here. The single `agent.go` call site is intentionally
> left for the system track to add (S3 owns the store; the agent loop is out of
> S3's territory), so until that line lands the trail is populated by direct
> `audit.Record` callers and tests, not the live loop.

## Reading

The `audit_query` builtin tool reads the persistent store with optional
`actor` / `action` / `resource` filters and returns the matching entries,
including the recorded why/args/result. It reflects what's on disk, so it works
across restarts.
