//! Prefix shape capture & miss diagnostics (Reasonix cache_shape, upgraded).
//!
//! DeepSeek (and similar automatic-prefix providers) reuse the **byte-stable**
//! system + tools prefix across turns. Comparing shapes explains *why* a cache
//! miss happened instead of only reporting hit/miss tokens.

use sha2::{Digest, Sha256};

/// Snapshot of the cache-stable request prefix.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct PrefixShape {
    pub system_hash: String,
    pub tools_hash: String,
    pub prefix_hash: String,
    /// Host-side log rewrite / compaction generation; bumps invalidate prefix.
    pub log_rewrite_version: u64,
    /// Rough tools-schema token estimate (bytes/4).
    pub tool_schema_tokens: u64,
}

/// Why the prefix changed between two turns (Reasonix + Lumen extras).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PrefixChangeReason {
    System,
    Tools,
    LogRewrite,
    /// First turn of a session (no previous shape).
    ColdStart,
}

impl PrefixChangeReason {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::System => "system",
            Self::Tools => "tools",
            Self::LogRewrite => "log_rewrite",
            Self::ColdStart => "cold_start",
        }
    }
}

/// Diagnostics for one turn's prefix + provider cache tokens.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct CacheDiagnostics {
    pub prefix_hash: String,
    pub prefix_changed: bool,
    pub change_reasons: Vec<PrefixChangeReason>,
    pub system_hash: String,
    pub tools_hash: String,
    pub log_rewrite_version: u64,
    pub tool_schema_tokens: u64,
    pub cache_hit_tokens: u64,
    pub cache_miss_tokens: u64,
}

fn short_hash(bytes: &[u8]) -> String {
    let digest = Sha256::digest(bytes);
    // 8 hex bytes — enough for diagnostics, cheap to log.
    digest
        .iter()
        .take(8)
        .map(|b| format!("{b:02x}"))
        .collect()
}

/// Capture prefix shape from system prompt text and a tools JSON blob
/// (already serialized tool schemas). Tool order is caller-normalized.
pub fn capture_shape(
    system_prompt: &str,
    tools_json: &str,
    log_rewrite_version: u64,
) -> PrefixShape {
    let system_hash = short_hash(system_prompt.as_bytes());
    let tools_hash = short_hash(tools_json.as_bytes());
    let mut prefix = Vec::with_capacity(system_prompt.len() + tools_json.len() + 1);
    prefix.extend_from_slice(system_prompt.as_bytes());
    prefix.push(0);
    prefix.extend_from_slice(tools_json.as_bytes());
    let prefix_hash = short_hash(&prefix);
    let tool_schema_tokens = estimate_tokens(tools_json);
    PrefixShape {
        system_hash,
        tools_hash,
        prefix_hash,
        log_rewrite_version,
        tool_schema_tokens,
    }
}

/// Rough token estimate for diagnostics (never billing).
///
/// ASCII-heavy text (schema JSON, English prose) tokenizes near 4 bytes/token,
/// but CJK is ~3 bytes per char at roughly 1 token every 1–2 chars — a pure
/// bytes/4 rule under-weights it… while counting raw bytes over-weights it by
/// ~3×. Estimate per character class: ASCII at 4 chars/token, everything else
/// at 1.5 chars/token (≈ two tokens per three CJK chars).
pub fn estimate_tokens(s: &str) -> u64 {
    if s.is_empty() {
        return 0;
    }
    let (ascii, wide) = s
        .chars()
        .fold((0u64, 0u64), |(ascii, wide), c| {
            if c.is_ascii() {
                (ascii + 1, wide)
            } else {
                (ascii, wide + 1)
            }
        });
    (ascii.div_ceil(4) + (wide * 2).div_ceil(3)).max(1)
}

/// Compare previous vs current shape; fold optional provider usage.
///
/// `cache_hit_tokens` / `cache_miss_tokens` come from the provider when known.
/// If only hit + total input are known, pass `cache_miss = input.saturating_sub(hit)`.
pub fn compare_shape(
    prev: Option<&PrefixShape>,
    cur: &PrefixShape,
    cache_hit_tokens: u64,
    cache_miss_tokens: u64,
) -> CacheDiagnostics {
    let mut reasons = Vec::new();
    let prefix_changed = match prev {
        None => {
            reasons.push(PrefixChangeReason::ColdStart);
            true
        }
        Some(p) => {
            if p.system_hash != cur.system_hash {
                reasons.push(PrefixChangeReason::System);
            }
            if p.tools_hash != cur.tools_hash {
                reasons.push(PrefixChangeReason::Tools);
            }
            if p.log_rewrite_version != cur.log_rewrite_version {
                reasons.push(PrefixChangeReason::LogRewrite);
            }
            !reasons.is_empty()
        }
    };
    CacheDiagnostics {
        prefix_hash: cur.prefix_hash.clone(),
        prefix_changed,
        change_reasons: reasons,
        system_hash: cur.system_hash.clone(),
        tools_hash: cur.tools_hash.clone(),
        log_rewrite_version: cur.log_rewrite_version,
        tool_schema_tokens: cur.tool_schema_tokens,
        cache_hit_tokens,
        cache_miss_tokens,
    }
}

/// Human-readable miss reasons for status / logs.
pub fn format_change_reasons(reasons: &[PrefixChangeReason]) -> String {
    if reasons.is_empty() {
        return "stable".to_string();
    }
    reasons
        .iter()
        .map(|r| r.as_str())
        .collect::<Vec<_>>()
        .join(",")
}

#[cfg(test)]
mod tests {
    #[test]
    fn estimate_tokens_ascii_stays_bytes_over_four() {
        assert_eq!(super::estimate_tokens(""), 0);
        assert_eq!(super::estimate_tokens("abcd"), 1);
        assert_eq!(super::estimate_tokens(&"x".repeat(400)), 100);
    }

    #[test]
    fn estimate_tokens_cjk_not_inflated_by_utf8_bytes() {
        // 100 CJK chars = 300 UTF-8 bytes. The old bytes/4 rule said 75;
        // per-char classing says ~67 (2 tokens per 3 chars) — and crucially
        // NOT the ~3x inflation a raw byte count would give (300).
        let s = "样".repeat(100);
        let est = super::estimate_tokens(&s);
        assert_eq!(est, 67);
    }

    use super::*;

    #[test]
    fn same_prefix_is_stable() {
        let a = capture_shape("sys", r#"[{"name":"t"}]"#, 0);
        let b = capture_shape("sys", r#"[{"name":"t"}]"#, 0);
        assert_eq!(a.prefix_hash, b.prefix_hash);
        let d = compare_shape(Some(&a), &b, 1000, 100);
        assert!(!d.prefix_changed);
        assert!(d.change_reasons.is_empty());
    }

    #[test]
    fn system_change_detected() {
        let a = capture_shape("sys-a", "[]", 0);
        let b = capture_shape("sys-b", "[]", 0);
        let d = compare_shape(Some(&a), &b, 0, 500);
        assert!(d.prefix_changed);
        assert!(d.change_reasons.contains(&PrefixChangeReason::System));
    }

    #[test]
    fn tools_change_detected() {
        let a = capture_shape("sys", r#"[{"name":"a"}]"#, 0);
        let b = capture_shape("sys", r#"[{"name":"b"}]"#, 0);
        let d = compare_shape(Some(&a), &b, 0, 500);
        assert!(d.change_reasons.contains(&PrefixChangeReason::Tools));
    }

    #[test]
    fn log_rewrite_detected() {
        let a = capture_shape("sys", "[]", 1);
        let b = capture_shape("sys", "[]", 2);
        let d = compare_shape(Some(&a), &b, 10, 10);
        assert!(d.change_reasons.contains(&PrefixChangeReason::LogRewrite));
    }

    #[test]
    fn cold_start_marked() {
        let b = capture_shape("sys", "[]", 0);
        let d = compare_shape(None, &b, 0, 100);
        assert!(d.change_reasons.contains(&PrefixChangeReason::ColdStart));
    }
}

// ============================================================================
// B1 property tests (DEBT-033): prefix determinism under state churn.
// ============================================================================
//
// The provider cache requires a byte-stable prefix (DeepSeek Context Caching:
// full unit match). These tests lock the invariants the renderer must uphold:
// same logical state -> identical fingerprint; tool order and rewrite version
// deliberately participate (they are part of the wire material); token
// estimates are deterministic and monotonic.

#[cfg(test)]
mod property_tests {
    use super::*;

    /// Tiny deterministic xorshift PRNG for property-style inputs (no deps).
    struct Rng(u64);

    impl Rng {
        fn next(&mut self) -> u64 {
            let mut x = self.0;
            x ^= x << 13;
            x ^= x >> 7;
            x ^= x << 17;
            self.0 = x;
            x
        }

        fn ascii(&mut self, len: usize) -> String {
            (0..len).map(|_| (b'a' + (self.next() % 26) as u8) as char).collect()
        }
    }

    #[test]
    fn capture_shape_is_deterministic_under_state_churn() {
        let mut rng = Rng(0xDEAD_BEEF);
        for _ in 0..100 {
            let sys_len = (rng.next() % 200 + 1) as usize;
            let tools_len = (rng.next() % 400 + 1) as usize;
            let sys = rng.ascii(sys_len);
            let tools = rng.ascii(tools_len);
            let rev = rng.next() % 5;
            let a = capture_shape(&sys, &tools, rev);
            let b = capture_shape(&sys, &tools, rev);
            assert_eq!(a.prefix_hash, b.prefix_hash, "prefix hash must be deterministic");
            assert_eq!(a.system_hash, b.system_hash);
            assert_eq!(a.tools_hash, b.tools_hash);
            assert_eq!(a.log_rewrite_version, rev);
        }
    }

    #[test]
    fn tools_order_is_deliberately_sensitive() {
        // Tool order is part of the provider request material: reordering
        // must change the fingerprint (cache identity) — never silently
        // stable. This is the byte-stability contract, not a flake.
        let sys = "system";
        let a = capture_shape(sys, r#"[{"name":"read"},{"name":"write"}]"#, 0);
        let b = capture_shape(sys, r#"[{"name":"write"},{"name":"read"}]"#, 0);
        assert_ne!(a.tools_hash, b.tools_hash);
        assert_ne!(a.prefix_hash, b.prefix_hash);
    }

    #[test]
    fn rewrite_version_is_diagnostics_not_wire_material() {
        // The rewrite counter is a LOCAL diagnostic axis: it never changes the
        // wire prefix (provider cache identity is untouched by a rewrite).
        // compare_shape detects it via the version field, not the hash.
        let a = capture_shape("system", "[]", 0);
        let b = capture_shape("system", "[]", 1);
        assert_eq!(a.prefix_hash, b.prefix_hash, "wire prefix must ignore local rewrite counter");
        assert_ne!(a.log_rewrite_version, b.log_rewrite_version);
    }

    #[test]
    fn estimate_tokens_is_deterministic_and_monotonic() {
        let mut rng = Rng(0xC0FFEE);
        let mut prev = 0u64;
        let mut acc = String::new();
        for _ in 0..50 {
            acc.push_str(&rng.ascii(10));
            let tokens = estimate_tokens(&acc);
            assert!(
                tokens >= prev,
                "longer input must not estimate fewer tokens (got {tokens} < {prev})"
            );
            assert!(
                tokens <= (acc.len() as u64).saturating_add(1),
                "estimate must not exceed byte count +1"
            );
            prev = tokens;
        }
        assert_eq!(estimate_tokens(""), 0);
        assert_eq!(estimate_tokens("abcd"), 1);
        assert_eq!(estimate_tokens(r#"{"a":"bcd"}"#), 3); // 12 ASCII chars -> ceil(12/4)
    }

    #[test]
    fn cjk_estimate_weights_wide_chars() {
        // 6 CJK chars ≈ 4 tokens (1.5 chars/token) — a pure bytes/4 rule would
        // under-weight to 1..2. This guards the estimate used by the staged
        // compaction policy on Chinese-heavy tool output.
        assert!(estimate_tokens("中文测试文本啊") >= 3);
    }
}
