//! Typed runtime profiles (master plan §0.1.7).
//!
//! `interactive_single_turn` is the default: short root tasks create no tree,
//! no durable ledger, no daemon, no advisor call. `governed_tree_development`
//! is the offline/development governed profile. `kairos_local` is the
//! long-task recovery profile.
//!
//! Upgrade is **one-way and non-downgradable** (INV-23): once a run enters a
//! governed profile it never returns to a lighter one, and a failed upgrade
//! surfaces as `Blocked(AdmissionUpgradeFailed)` — never as a best-effort
//! continuation. Governance is added by upgrading; no switch can *reduce* the
//! permission checks, evidence requirements, no-replay or budget reservation
//! of a governed profile.

use std::str::FromStr;

pub const RUNTIME_PROFILE_SCHEMA_VERSION: u16 = 1;

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RuntimeProfile {
    /// Default: short root task, single actor turn, ephemeral admission
    /// snapshot, no tree/lease/ledger UI.
    InteractiveSingleTurn = 0,
    /// Dev/offline governed tree: TaskTree, ceiling, budget, ledger,
    /// ContextManifest, fault injection and tree UX.
    GovernedTreeDevelopment = 1,
    /// Local recovery drills: operation lease, journal/outbox, fake clock,
    /// operator freeze.
    KairosLocal = 2,
}

impl RuntimeProfile {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::InteractiveSingleTurn => "interactive_single_turn",
            Self::GovernedTreeDevelopment => "governed_tree_development",
            Self::KairosLocal => "kairos_local",
        }
    }

    /// The default profile for any new run.
    pub fn default_profile() -> Self {
        Self::InteractiveSingleTurn
    }

    /// Parse a profile name (manifest `admission_profile` string) and reject
    /// unknown names — a manifest naming a profile this binary does not know
    /// fails closed.
    pub fn parse_validated(name: &str) -> Result<Self, ProfileDeny> {
        match name {
            "interactive_single_turn" => Ok(Self::InteractiveSingleTurn),
            "governed_tree_development" => Ok(Self::GovernedTreeDevelopment),
            "kairos_local" => Ok(Self::KairosLocal),
            other => Err(ProfileDeny::UnknownProfile(other.to_string())),
        }
    }

    /// One-way, non-downgradable upgrade. `interactive → governed → kairos`;
    /// anything else (including any downgrade) is denied with
    /// `AdmissionUpgradeFailed`.
    pub fn upgrade(self, requested: RuntimeProfile) -> Result<RuntimeProfile, ProfileDeny> {
        match (self, requested) {
            (current, next) if current == next => Ok(current),
            (Self::InteractiveSingleTurn, Self::GovernedTreeDevelopment)
            | (Self::GovernedTreeDevelopment, Self::KairosLocal)
            | (Self::InteractiveSingleTurn, Self::KairosLocal) => Ok(requested),
            (current, _) => Err(ProfileDeny::AdmissionUpgradeFailed {
                from: current.as_str().to_string(),
                requested: requested.as_str().to_string(),
            }),
        }
    }

    pub fn may_spawn_children(self) -> bool {
        matches!(self, Self::GovernedTreeDevelopment | Self::KairosLocal)
    }

    pub fn requires_durable_ledger(self) -> bool {
        matches!(self, Self::GovernedTreeDevelopment | Self::KairosLocal)
    }

    pub fn may_use_daemon(self) -> bool {
        matches!(self, Self::KairosLocal)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ProfileDeny {
    UnknownProfile(String),
    AdmissionUpgradeFailed { from: String, requested: String },
}

impl ProfileDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::UnknownProfile(_) => "profile.unknown",
            Self::AdmissionUpgradeFailed { .. } => "profile.admission_upgrade_failed",
        }
    }
}

impl std::fmt::Display for ProfileDeny {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::UnknownProfile(name) => write!(f, "{}: {name}", self.code()),
            Self::AdmissionUpgradeFailed { from, requested } => {
                write!(f, "{}: {from} -> {requested}", self.code())
            }
        }
    }
}

impl FromStr for RuntimeProfile {
    type Err = ProfileDeny;
    fn from_str(s: &str) -> Result<Self, Self::Err> {
        Self::parse_validated(s)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_profile_is_interactive_single_turn() {
        assert_eq!(
            RuntimeProfile::default_profile(),
            RuntimeProfile::InteractiveSingleTurn
        );
    }

    #[test]
    fn parse_validated_accepts_three_profiles_and_rejects_unknown() {
        assert_eq!(
            RuntimeProfile::parse_validated("interactive_single_turn").expect("i"),
            RuntimeProfile::InteractiveSingleTurn
        );
        assert_eq!(
            RuntimeProfile::parse_validated("governed_tree_development").expect("g"),
            RuntimeProfile::GovernedTreeDevelopment
        );
        assert_eq!(
            RuntimeProfile::parse_validated("kairos_local").expect("k"),
            RuntimeProfile::KairosLocal
        );
        let err = RuntimeProfile::parse_validated("no_such_profile").unwrap_err();
        assert_eq!(err.code(), "profile.unknown");
    }

    #[test]
    fn upgrade_is_one_way_and_non_downgradable() {
        let interactive = RuntimeProfile::InteractiveSingleTurn;
        assert_eq!(
            interactive.upgrade(RuntimeProfile::GovernedTreeDevelopment).expect("up"),
            RuntimeProfile::GovernedTreeDevelopment
        );
        assert_eq!(
            interactive.upgrade(RuntimeProfile::KairosLocal).expect("up2"),
            RuntimeProfile::KairosLocal
        );
        // Same profile is idempotent.
        assert_eq!(
            interactive.upgrade(RuntimeProfile::InteractiveSingleTurn).expect("same"),
            RuntimeProfile::InteractiveSingleTurn
        );
        // Downgrades fail with the admission-upgrade-failed code (Blocked).
        let governed = RuntimeProfile::GovernedTreeDevelopment;
        let err = governed.upgrade(RuntimeProfile::InteractiveSingleTurn).unwrap_err();
        assert_eq!(err.code(), "profile.admission_upgrade_failed");
        assert_eq!(
            err,
            ProfileDeny::AdmissionUpgradeFailed {
                from: "governed_tree_development".into(),
                requested: "interactive_single_turn".into(),
            }
        );
        let kairos = RuntimeProfile::KairosLocal;
        assert_eq!(
            kairos.upgrade(RuntimeProfile::GovernedTreeDevelopment).unwrap_err(),
            ProfileDeny::AdmissionUpgradeFailed {
                from: "kairos_local".into(),
                requested: "governed_tree_development".into(),
            }
        );
        assert_eq!(
            kairos.upgrade(RuntimeProfile::InteractiveSingleTurn).unwrap_err(),
            ProfileDeny::AdmissionUpgradeFailed {
                from: "kairos_local".into(),
                requested: "interactive_single_turn".into(),
            }
        );
    }

    #[test]
    fn capability_bits_follow_profile_strength() {
        let interactive = RuntimeProfile::InteractiveSingleTurn;
        assert!(!interactive.may_spawn_children());
        assert!(!interactive.requires_durable_ledger());
        assert!(!interactive.may_use_daemon());
        let governed = RuntimeProfile::GovernedTreeDevelopment;
        assert!(governed.may_spawn_children());
        assert!(governed.requires_durable_ledger());
        assert!(!governed.may_use_daemon());
        let kairos = RuntimeProfile::KairosLocal;
        assert!(kairos.may_spawn_children());
        assert!(kairos.requires_durable_ledger());
        assert!(kairos.may_use_daemon());
    }
}
