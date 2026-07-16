//! Configuration for verify-after-edit.

/// Verify configuration (mirrors lumen.toml `[verify]`).
#[derive(Debug, Clone)]
pub struct Config {
    /// Whether verify-after-edit is enabled.
    pub enabled: bool,
    /// Maximum repair cycles before giving up (default 3).
    pub max_repair: u32,
    /// Per-step timeout in seconds (default 30).
    pub timeout_secs: u64,
    /// Verification scope: "changed-pkg" or "workspace".
    pub scope: String,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            enabled: true,
            max_repair: 3,
            timeout_secs: 30,
            scope: "changed-pkg".to_string(),
        }
    }
}
