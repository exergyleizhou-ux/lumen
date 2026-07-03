# CSswitch Learnings & Ports to Lumen (extreme optimization)

Date: 2026-07-03
Source: https://github.com/SuperJJ007/CSswitch.git (cloned /tmp/csswitch)
Focus: recent bug fixes (v0.2.1 login defects, v0.2.0 idempotency, #3 401/403 hang) + all superior details in security, proxy, process, docs, tests.

## Critical Bug Fixes - Status in Lumen

1. **Login URL parse (multi-line stdout from `claude-science url`)**  
   CSswitch: added `first_http_url` (only first valid http(s) line; ignore notes).  
   **Lumen**: `launcher.URL()` already takes first http/https line. Ported pre-this-session.  
   Evidence: code in internal/science/launcher/launcher.go

2. **Health bypass / no self-heal (daemon alive but login broken)**  
   CSswitch v0.2.1: health checks `login_intact` (reuse read_intact) first; if not intact, stop + fallthrough to ensure (preserve org).  
   **Lumen**: `launcher.Running()` gates on `oauth.IsLoginIntact(dataDir)` → false forces repair. Self-heal in Start/ensure.  
   Enhanced further: strict json + fallback token parse for status "running".

3. **Switching-org hang: CONNECT 403 vs 401**  
   CSswitch: targeted fast-fail on anthropic/claude.* hosts → 401 (not 403) so operon treats logged-out fast.  
   **Lumen**: internal/science/proxy/connect.go `handleConnect` does exactly: isBlockedHost → 401 logged-out fast-fail.  
   Docs in findings/ switched from CSswitch too.

No remaining same bugs. Lumen (Go science bridge + proxy) had absorbed core before full deep dive.

## Everything Better from CSswitch - Catalog + What We Extreme-Optimized

### Security / Guards (Ironclad FS)
- lstat (symlink_metadata) before EVERY read/write/mkdir/remove on config, keys, .enc, dirs, logs. Refuse symlink.
- Dirs: 0700 create + explicit set_permissions reset.
- Files: 0600 at open (O_EXCL create) + post-rename reset. Never trust prior perms.
- Atomic write: .tmp-pid-tid + O_CREATE|O_EXCL + write + sync_all + rename (no truncate race).
- Global mutex on update (load-mod-save) to prevent concurrent partials.
- No reliance on cwd; walk from exe/resource for asset/script.
- Redact secrets on any tail/error to UI/logs.
- **Ported extreme to lumen**:
  - science/config/config.go: O_EXCL+Sync+seq tmp, ensureDir is-dir+0700, more perm resets, added roundtrip+symlink+perm tests.
  - Existing: oauth/guard + forge (Lstat/assert on key/tok/org), research/seed, migrate, fileutil/SafeWrite, guard ports 8765.
  - launcher/runtime already assert ports, data isolated.

### Proxy
- Model map + caps + display names (borrow claude-* ids for selector flat list).
- resolve_model: exact, stripped date, prefix, fallback.
- clamp max_tokens.
- Retry only on transient (conn/read/empty), never on HTTP 4xx/5xx (original status passthru).
- CONNECT fast-fail targeted + 401 semantics.
- Full anthropic<->openai for qwen (tool_use preserved via SSE).
- Path secret auth, strip all inbound keys, loopback only.
- **Lumen already had near-identical**: providers.go has Models/ModelMap/ModelCaps, upstream.go retry 4x + 800ms backoff + jitter log + not retry >=400. connect.go 401. Good. Kept/extended.

### Process / Launcher / One-Key Idempotency
- ensure: key fingerprint for change detect → restart only on fp/provider/port delta + health.
- Persistent secret (path-secret) across restarts (avoid 403 on sandbox).
- ProxyAction / LoginAction accurate (Reused vs Restarted/Created/Repaired) for UI text.
- pkill strict: abs script path regex + port (not loose name).
- Stop reports real errs (no fake success).
- Mode switch: stop first (transactional) then persist.
- Health + intact before reuse.
- **Lumen had**: keyFP + reuse in runtime/runtime.go proxy start, secret persist, Reused/Restarted, guard asserts, intact gate, login shell not directly needed (bin passed).
- Enhanced: stricter status running parse (json first).

### Docs & Culture
- CLAUDE.md: explicit IRON LAWS (never touch real ~/.claude-science, no token copy, isolated ports/HOME, test not touch science).
- findings/: root cause + repro + evidence + live verify for each bug (e.g. switching-hang.md).
- Verified facts, known-issues tracked separately.
- **Lumen**: has threat-model.md, AUDIT, sandbox.md, security_incident, docs/superpowers. 
- **Added**: this findings/csswitch-learnings-and-ports.md ; recommend iron-laws section in README or threat-model.

### Tests & Ops
- Unit for first_http, login_intact, symlink attacks, atomic writes, perms reset.
- Proxy tests (connect asserts 401, stream, auth).
- doctor/verify-proxy/self-test scripts; launchd maint (read-only).
- **Lumen**: rich Go tests, goal_evidence_test (pure CLI sole producer, prebuilt binary), integration. 
- Ported: more config tests (symlink, roundtrip, perm reset). Use prebuilt $SCRATCH/lumen for E2E.

## Changes Made in This Session (extreme)
- Config hardening: stricter O_EXCL atomic + fsync + uniq seq tmp, ensure is-dir check +0700, enhanced tests.
- Launcher: stricter status "running" parse (json preferred + fallback).
- Runtime state.json: hardened saveState with lstat assert + O_EXCL+Sync+rename+perm reset (was plain WriteFile).
- Docs: added this learnings file.
- Cleaned stray root 'lumen' build artifact; fresh prebuilt goal 6/6 + doctor verifications.
- Verified no matching login/401 bugs remained; many details already superior or matched in Go rewrite.
- All per superpowers: systematic (root cause via git/reads), TDD (test added first-ish, run observed), verification (build/test before/after).

## Recommendations for Further Extreme
- Port CSswitch test_proxy_connect.py style asserts to lumen proxy_test.
- Add more lstat guards to paths writes (logs/state) if direct os.Create used.
- Consider native Rust forge or keep Go (lumen is Go primary).
- GUI self-heal + accurate actions already good in science/gui.
- Keep iron laws in every session start (like CLAUDE.md).

Lumen now stronger in all areas by adopting CSswitch's battle-hardened details while retaining its Go agent/eval/research strengths.
