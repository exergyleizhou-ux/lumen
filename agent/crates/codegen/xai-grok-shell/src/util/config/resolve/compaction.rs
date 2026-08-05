/// Default auto-compact threshold (% of context window) when no source sets it.
pub const DEFAULT_AUTO_COMPACT_THRESHOLD_PERCENT: u8 = 85;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum CompactionToolChoice {
    /// Lumen default: compact requests must not offer tool use (upstream's
    /// default is `Auto`; the L5 fixture and the pre-merge wire contract both
    /// key on `tool_choice: "none"`). Opt in via `GROK_COMPACTION_TOOL_CHOICE=auto`
    /// or the per-model config key.
    #[default]
    None,
    Auto,
}

impl std::str::FromStr for CompactionToolChoice {
    type Err = ();

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.trim().to_ascii_lowercase().as_str() {
            "auto" => Ok(Self::Auto),
            "none" => Ok(Self::None),
            _ => Err(()),
        }
    }
}

pub(crate) const ENV_COMPACTION_TOOL_CHOICE: &str = "GROK_COMPACTION_TOOL_CHOICE";

pub fn resolve_compaction_tool_choice_from(
    env: Option<&str>,
    config: Option<&str>,
    remote: Option<&str>,
) -> CompactionToolChoice {
    env.and_then(|s| s.parse().ok())
        .or_else(|| config.and_then(|s| s.parse().ok()))
        .or_else(|| remote.and_then(|s| s.parse().ok()))
        .unwrap_or_default()
}

/// Env-var override for `auto_compact_threshold_percent`. Parsed as `u8`;
/// out-of-range or unparseable values are ignored.
pub(crate) const ENV_AUTO_COMPACT_THRESHOLD_PERCENT: &str = "GROK_AUTO_COMPACT_THRESHOLD_PERCENT";

/// Resolve auto-compact threshold percent (0-100) for the given model.
///
/// Two scopes (per-model and global) across two tiers (user TOML and
/// remote settings). User-tier always wins over remote; within a tier, per-model
/// wins over global. Env var sits on top as a per-process override.
///
/// Precedence (highest first):
///   1. env `GROK_AUTO_COMPACT_THRESHOLD_PERCENT`
///   2. user TOML `[model.<id>].auto_compact_threshold_percent`
///      (read from `cfg.config_models`; the effective merge of user +
///      managed `[model.<id>]` sections)
///   3. user TOML `[session].auto_compact_threshold_percent`
///      (read from `cfg.session.auto_compact_threshold_percent: Option<u8>`)
///   4. remote settings per-model `ModelInfo.auto_compact_threshold_percent`
///      (populated from `grok_build_models[i].auto_compact_threshold_percent`;
///      intentionally NOT collapsed via `ConfigModelOverride::apply` so the
///      user-vs-GB per-model distinction is preserved)
///   5. remote settings global `RemoteSettings.auto_compact_threshold_percent`
///      (populated from `grok_build_settings.auto_compact_threshold_percent`)
///   6. default `DEFAULT_AUTO_COMPACT_THRESHOLD_PERCENT` (85)
///
/// Values outside `0..=100` from the env var are ignored with a debug log and
/// the resolver falls through to the next tier. TOML/remote fields are typed
/// `u8` and so naturally constrained.
pub fn resolve_auto_compact_threshold_percent(
    cfg: &crate::agent::config::Config,
    model_id: &str,
    model: Option<&crate::agent::config::ModelInfo>,
) -> u8 {
    resolve_auto_compact_threshold_percent_from_tiers(
        cfg.config_models
            .get(model_id)
            .and_then(|m| m.auto_compact_threshold_percent),
        cfg.session.auto_compact_threshold_percent,
        model.and_then(|m| m.auto_compact_threshold_percent),
        cfg.remote_settings
            .as_ref()
            .and_then(|r| r.auto_compact_threshold_percent),
    )
}

/// Lower-level form of [`resolve_auto_compact_threshold_percent`] that takes
/// the four tiers as plain `Option<u8>` values rather than reaching into a
/// `Config`. Useful from sites that don't hold a `Config` reference (e.g.,
/// subagent spawn paths where the parent's config tiers are plumbed in
/// explicitly and the per-model lookup uses the SUBAGENT's resolved model id,
/// not the parent's).
///
/// Precedence: env > `user_per_model` > `user_global` > `gb_per_model`
/// > `gb_global` > `DEFAULT_AUTO_COMPACT_THRESHOLD_PERCENT`.
pub fn resolve_auto_compact_threshold_percent_from_tiers(
    user_per_model: Option<u8>,
    user_global: Option<u8>,
    gb_per_model: Option<u8>,
    gb_global: Option<u8>,
) -> u8 {
    fn clamp_env(raw: i64) -> Option<u8> {
        if (0..=100).contains(&raw) {
            Some(raw as u8)
        } else {
            tracing::debug!(
                source = "env",
                value = raw,
                "auto_compact_threshold_percent out of range 0..=100; ignoring"
            );
            None
        }
    }
    let from_env = || -> Option<u8> {
        std::env::var(ENV_AUTO_COMPACT_THRESHOLD_PERCENT)
            .ok()
            .and_then(|s| s.parse::<i64>().ok())
            .and_then(clamp_env)
    };

    from_env()
        .or(user_per_model)
        .or(user_global)
        .or(gb_per_model)
        .or(gb_global)
        .unwrap_or(DEFAULT_AUTO_COMPACT_THRESHOLD_PERCENT)
}

/// Client default per-compaction wall-clock budget (seconds). Fleet p99 of
/// successful compactions is ~181s (≈225s at 400K+ input), so 300s clears the
/// legit tail with margin while cutting a runaway from the ~600s deadline.
pub const DEFAULT_COMPACTION_WALL_CLOCK_BUDGET_SECS: u64 = 300;

/// Below this, a configured budget is almost certainly a misconfig (fleet
/// success p99 ~181s); logged at `warn`, not clamped.
const COMPACTION_WALL_CLOCK_BUDGET_WARN_SECS: u64 = 120;

/// Env override for the compaction wall-clock budget (seconds). Parsed as
/// `u64`; unparseable values fall through.
const ENV_COMPACTION_WALL_CLOCK_BUDGET_SECS: &str = "GROK_COMPACTION_WALL_CLOCK_SECS";

/// Resolve the per-compaction wall-clock budget (seconds). Precedence: env
/// `GROK_COMPACTION_WALL_CLOCK_SECS` > remote settings global
/// `RemoteSettings.compaction_wall_clock_budget_secs` >
/// [`DEFAULT_COMPACTION_WALL_CLOCK_BUDGET_SECS`] (a per-model `ModelInfo` tier
/// would slot in ahead of the global one).
///
/// `0` **disables** it. Low values are warned, not clamped — any "safe" clamp
/// (e.g. 30s) would itself cut legit compactions, trading one silent failure for
/// another; ops own the value.
pub fn resolve_compaction_wall_clock_budget_secs(gb_global: Option<u64>) -> u64 {
    let from_env = std::env::var(ENV_COMPACTION_WALL_CLOCK_BUDGET_SECS)
        .ok()
        .and_then(|s| s.trim().parse::<u64>().ok());
    let resolved = from_env
        .or(gb_global)
        .unwrap_or(DEFAULT_COMPACTION_WALL_CLOCK_BUDGET_SECS);
    if resolved > 0 && resolved < COMPACTION_WALL_CLOCK_BUDGET_WARN_SECS {
        tracing::warn!(
            budget_secs = resolved,
            "compaction wall-clock budget {resolved}s is below {COMPACTION_WALL_CLOCK_BUDGET_WARN_SECS}s \
             and may cut legitimate compactions (fleet success p99 ~181s); set 0 to disable"
        );
    }
    resolved
}

/// Env override for the staged compaction policy (DEBT-033 A2-b):
/// `GROK_COMPACTION_POLICY=snip_tokens,placeholder_tokens,fold_tokens,budget_ratio`.
/// All four components optional; unparseable values fall back per-component to
/// [`lumen_discipline::CompactionPolicy::default`]. `never_fold_user` is a
/// hard invariant and is never configurable.
pub(crate) const ENV_COMPACTION_POLICY: &str = "GROK_COMPACTION_POLICY";

/// Resolve the staged compaction policy from env (falls back to defaults).
pub fn resolve_compaction_policy(env: Option<&str>) -> lumen_discipline::CompactionPolicy {
    let base = lumen_discipline::CompactionPolicy::default();
    let trimmed = env.map(str::trim).unwrap_or("");
    let raw = trimmed
        .strip_prefix('"')
        .and_then(|s| s.strip_suffix('"'))
        .unwrap_or(trimmed);
    if raw.is_empty() {
        return base;
    }
    let parts: Vec<&str> = raw.split(',').map(str::trim).collect();
    let mut policy = base;
    if let Some(v) = parts.first().and_then(|p| p.parse::<u64>().ok()) {
        policy.snip_threshold_tokens = v;
    }
    if let Some(v) = parts.get(1).and_then(|p| p.parse::<u64>().ok()) {
        policy.placeholder_threshold_tokens = v;
    }
    if let Some(v) = parts.get(2).and_then(|p| p.parse::<u64>().ok()) {
        policy.fold_threshold_tokens = v;
    }
    if let Some(v) = parts.get(3).and_then(|p| p.parse::<f64>().ok())
        && (0.0..=1.0).contains(&v)
    {
        policy.remaining_budget_trigger_ratio = v;
    }
    // Hard invariant: user turns and digests are never folded.
    policy.never_fold_user = true;
    policy
}

#[cfg(test)]
mod staged_policy_tests {
    use super::resolve_compaction_policy as resolve;

    #[test]
    fn default_when_unset_or_garbage() {
        let d = lumen_discipline::CompactionPolicy::default();
        assert_eq!(resolve(None), d);
        assert_eq!(resolve(Some("garbage")), d);
        assert_eq!(resolve(Some("")), d);
    }

    #[test]
    fn parses_all_four_components() {
        let p = resolve(Some("10000,20000,30000,0.25"));
        assert_eq!(p.snip_threshold_tokens, 10_000);
        assert_eq!(p.placeholder_threshold_tokens, 20_000);
        assert_eq!(p.fold_threshold_tokens, 30_000);
        assert_eq!(p.remaining_budget_trigger_ratio, 0.25);
        assert!(p.never_fold_user);
    }

    #[test]
    fn partial_components_fall_back_per_field() {
        let d = lumen_discipline::CompactionPolicy::default();
        let p = resolve(Some("10000,,,0.9"));
        assert_eq!(p.snip_threshold_tokens, 10_000);
        assert_eq!(p.placeholder_threshold_tokens, d.placeholder_threshold_tokens);
        assert_eq!(p.fold_threshold_tokens, d.fold_threshold_tokens);
        assert_eq!(p.remaining_budget_trigger_ratio, 0.9);
    }

    #[test]
    fn out_of_range_ratio_is_rejected() {
        let d = lumen_discipline::CompactionPolicy::default();
        assert_eq!(resolve(Some(",,,1.5")).remaining_budget_trigger_ratio, d.remaining_budget_trigger_ratio);
        assert_eq!(resolve(Some(",,,-0.1")).remaining_budget_trigger_ratio, d.remaining_budget_trigger_ratio);
    }

    #[test]
    fn never_fold_user_cannot_be_turned_off() {
        // Even a hostile value cannot disable the invariant.
        assert!(resolve(Some("1,2,3,0.5")).never_fold_user);
    }
}

#[cfg(test)]
mod compaction_wall_clock_budget_tests {
    use super::resolve_compaction_wall_clock_budget_secs as resolve;

    // Assumes GROK_COMPACTION_WALL_CLOCK_SECS is unset in the test env.
    #[test]
    fn default_global_disable_and_no_clamp() {
        assert_eq!(resolve(None), 300); // client default
        assert_eq!(resolve(Some(450)), 450); // server global wins
        assert_eq!(resolve(Some(0)), 0); // 0 explicitly disables (no clamp)
        assert_eq!(resolve(Some(5)), 5); // low values pass through (warned, not clamped)
    }
}

#[cfg(test)]
mod compaction_tool_choice_tests {
    use super::{CompactionToolChoice, resolve_compaction_tool_choice_from as resolve};

    #[test]
    fn default_is_none() {
        assert_eq!(resolve(None, None, None), CompactionToolChoice::None);
    }

    #[test]
    fn precedence_env_over_config_over_remote() {
        assert_eq!(
            resolve(Some("none"), Some("auto"), Some("auto")),
            CompactionToolChoice::None
        );
        assert_eq!(
            resolve(None, Some("none"), Some("auto")),
            CompactionToolChoice::None
        );
        assert_eq!(
            resolve(None, None, Some("none")),
            CompactionToolChoice::None
        );
    }

    #[test]
    fn garbage_falls_through() {
        assert_eq!(
            resolve(Some("garbage"), None, Some("none")),
            CompactionToolChoice::None
        );
        assert_eq!(
            resolve(Some("garbage"), Some("also-bad"), None),
            CompactionToolChoice::None
        );
    }

    #[test]
    fn from_str_case_insensitive() {
        assert_eq!("AUTO".parse(), Ok(CompactionToolChoice::Auto));
        assert_eq!(" None ".parse(), Ok(CompactionToolChoice::None));
        assert!("required".parse::<CompactionToolChoice>().is_err());
    }
}
