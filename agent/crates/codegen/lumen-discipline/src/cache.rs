//! Cache usage formatting for status / headless (DeepSeek-friendly).

/// Token usage slice relevant to prompt cache visibility.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct CacheUsage {
    pub input_tokens: u64,
    pub cache_read_tokens: u64,
    pub output_tokens: u64,
}

/// One-line summary for status bar / session-info.
///
/// Example: `cache 12.4k/50.0k (24%) · out 800`
pub fn format_cache_line(u: CacheUsage) -> String {
    let cached = u.cache_read_tokens;
    let input = u.input_tokens.max(cached);
    let pct = if input == 0 {
        0
    } else {
        ((cached as f64 / input as f64) * 100.0).round() as u64
    };
    format!(
        "cache {}/{} ({}%) · out {}",
        fmt_k(cached),
        fmt_k(input),
        pct,
        fmt_k(u.output_tokens)
    )
}

fn fmt_k(n: u64) -> String {
    if n >= 1000 {
        format!("{:.1}k", n as f64 / 1000.0)
    } else {
        n.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn format_shows_percent() {
        let line = format_cache_line(CacheUsage {
            input_tokens: 50_000,
            cache_read_tokens: 12_400,
            output_tokens: 800,
        });
        assert!(line.contains("cache"), "{line}");
        assert!(line.contains("%"), "{line}");
        assert!(line.contains("out"), "{line}");
    }

    #[test]
    fn zero_input_no_div0() {
        let line = format_cache_line(CacheUsage::default());
        assert!(line.contains("(0%)"), "{line}");
    }

    #[test]
    fn thousands_use_one_decimal_without_duplicate_ranges() {
        assert_eq!(fmt_k(999), "999");
        assert_eq!(fmt_k(1_000), "1.0k");
        assert_eq!(fmt_k(10_000), "10.0k");
    }
}
