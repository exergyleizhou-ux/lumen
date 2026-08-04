//! Installed grok CLI version identity.
//!
//! The Lumen product line (2.x) and the upstream protocol identity (0.x) are
//! deliberately split: this crate keeps the upstream grok CLI protocol
//! identity (currently 0.2.116, never bumped by `release.sh`), while the
//! Lumen product version is carried by the product crates (e.g.
//! `xai-grok-update`, which `release.sh` *does* bump). When unstamped,
//! [`VERSION`] falls back to this crate's own package version — the upstream
//! identity, not the Lumen product version.

use semver::Version;

pub const TEST_VERSION_ENV: &str = "GROK_TEST_VERSION";

/// This crate's own package version — the upstream protocol identity used
/// as the unstamped fallback for [`VERSION`].
pub const PACKAGE_VERSION: &str = env!("CARGO_PKG_VERSION");

pub const VERSION: &str = match option_env!("GROK_VERSION") {
    Some(v) => v,
    None => PACKAGE_VERSION,
};

/// [`TEST_VERSION_ENV`] override first, then [`VERSION`]. Trimmed so
/// non-semver-aware callers can pass the result straight into parsing.
pub fn installed() -> String {
    std::env::var(TEST_VERSION_ENV)
        .map(|v| v.trim().to_string())
        .unwrap_or_else(|_| VERSION.to_string())
}

pub fn installed_semver() -> Result<Version, semver::Error> {
    Version::parse(&installed())
}

/// Format the compiled version with a channel label for user-facing display.
///
/// `channel_label` is a pre-formatted suffix such as `" [alpha]"`, `" [stable]"`,
/// or `""` (empty when no cached pointer is available). Obtain it from
/// `xai_grok_update::channel_label()`.
///
/// Example: `"0.2.5 [stable]"` or `"0.2.5 [alpha]"`.
pub fn display_version(channel_label: &str) -> String {
    format!("{}{}", VERSION, channel_label)
}

/// Format a version-with-commit string with a channel label.
///
/// Same semantics as [`display_version`] but for the full
/// `"0.2.5 (abc1234)"` string.
pub fn display_version_with_commit(version_with_commit: &str, channel_label: &str) -> String {
    format!("{}{}", version_with_commit, channel_label)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The unstamped fallback must be this crate's own package version
    /// (the upstream protocol identity), never a hardcoded string that
    /// could drift from Cargo.
    #[test]
    fn unstamped_version_falls_back_to_own_package_version() {
        if option_env!("GROK_VERSION").is_none() {
            assert_eq!(VERSION, PACKAGE_VERSION);
        }
    }

    /// Display formatting invariant matrix — verifies label appending
    /// works correctly across all label states (alpha, stable, empty).
    #[test]
    fn test_display_version_formatting_matrix() {
        let cases: &[(&str, &str, &str)] = &[
            // (version_with_commit,    label,        expected_suffix)
            ("0.2.5 (abc1234)", " [alpha]", "0.2.5 (abc1234) [alpha]"),
            ("0.2.5 (abc1234)", " [stable]", "0.2.5 (abc1234) [stable]"),
            ("0.2.5 (abc1234)", "", "0.2.5 (abc1234)"),
            (
                "0.1.220-alpha.2 (def0)",
                " [alpha]",
                "0.1.220-alpha.2 (def0) [alpha]",
            ),
        ];
        for (vwc, label, expected) in cases {
            assert_eq!(
                display_version_with_commit(vwc, label),
                *expected,
                "display_version_with_commit({:?}, {:?})",
                vwc,
                label,
            );
        }
        // display_version uses compiled VERSION — just verify the label appends
        assert_eq!(display_version(""), VERSION);
        assert!(display_version(" [stable]").ends_with("[stable]"));
    }
}
