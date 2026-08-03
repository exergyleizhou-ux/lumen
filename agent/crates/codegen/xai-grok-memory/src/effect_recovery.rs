//! NG-03C/K4 — EffectRecoveryClass.
//!
//! Exactly-once against an arbitrary external world is impossible. What IS
//! possible is one of three delivery semantics, each with a cost:
//! at-most-once (may lose, never duplicates) / at-least-once + world
//! deduplication / at-most-once + reconcile after a world probe.
//!
//! So the recovery class of an effect is decided by what the EXTERNAL WORLD
//! offers (idempotency key? probe?) — not by how severe we judge the effect.
//! The recovery procedure is then *derived* from the class instead of being
//! looked up in a hand-written table:
//!
//! | class      | crash-safe action        | unattended |
//! |------------|--------------------------|------------|
//! | Pure       | rerun                    | yes        |
//! | Idempotent | rerun with the same key  | yes        |
//! | Queryable  | probe, then resume/rerun | yes        |
//! | Opaque     | Frozen                   | no         |
//!
//! Consequence: the upper bound of the Frozen rate is the proportion of
//! Opaque effects — computable at design time, not observable only in soak.
//! A compensation only counts as compensation when its own class is
//! Idempotent or Queryable; an Opaque "compensation" is not a compensation.

use crate::canonical::{CanonicalError, CanonicalRecord, CanonicalValue, ENCODING_REVISION};
use sha2::{Digest, Sha256};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum EffectRecoveryClass {
    /// Does not change the world → unconditionally retryable.
    Pure,
    /// The world deduplicates by key → at-least-once is safe.
    Idempotent { key: String },
    /// The world can be asked about the effect's outcome → probe first.
    Queryable { probe: String },
    /// The world neither deduplicates nor answers → unknown means Frozen.
    Opaque,
}

impl EffectRecoveryClass {
    fn as_str(&self) -> &'static str {
        match self {
            EffectRecoveryClass::Pure => "pure",
            EffectRecoveryClass::Idempotent { .. } => "idempotent",
            EffectRecoveryClass::Queryable { .. } => "queryable",
            EffectRecoveryClass::Opaque => "opaque",
        }
    }

    /// The single safe crash action for this class (derived, not looked up).
    pub fn crash_safe_action(&self) -> CrashSafeAction {
        match self {
            EffectRecoveryClass::Pure => CrashSafeAction::Rerun,
            EffectRecoveryClass::Idempotent { key } => CrashSafeAction::RerunWithKey {
                key: key.clone(),
            },
            EffectRecoveryClass::Queryable { probe } => CrashSafeAction::ProbeThenResume {
                probe: probe.clone(),
            },
            EffectRecoveryClass::Opaque => CrashSafeAction::Frozen,
        }
    }

    /// Whether this class may run unattended after a crash (no human).
    pub fn unattended_safe(&self) -> bool {
        !matches!(self, EffectRecoveryClass::Opaque)
    }

    /// Whether an effect of this class may act as a compensation. An Opaque
    /// compensation is not a compensation: its outcome is unknowable, so it
    /// cannot restore the world in a verifiable way.
    pub fn compensable(&self) -> bool {
        matches!(
            self,
            EffectRecoveryClass::Idempotent { .. } | EffectRecoveryClass::Queryable { .. }
        )
    }

    pub fn canonical_bytes(&self) -> Result<Vec<u8>, CanonicalError> {
        let record = CanonicalRecord::new("effect-recovery-class")
            .field("class", CanonicalValue::str(self.as_str()))
            .field(
                "key",
                match self {
                    EffectRecoveryClass::Idempotent { key } => CanonicalValue::str(key),
                    _ => CanonicalValue::Null,
                },
            )
            .field(
                "probe",
                match self {
                    EffectRecoveryClass::Queryable { probe } => CanonicalValue::str(probe),
                    _ => CanonicalValue::Null,
                },
            )
            .field("encoding_revision", CanonicalValue::U64(u64::from(ENCODING_REVISION)));
        record.canonical_bytes()
    }

    pub fn class_hash(&self) -> Result<String, CanonicalError> {
        let digest = Sha256::digest(self.canonical_bytes()?);
        Ok(format!("sha256:{digest:x}"))
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CrashSafeAction {
    Rerun,
    RerunWithKey { key: String },
    ProbeThenResume { probe: String },
    Frozen,
}

/// Design-time accounting: the Frozen rate upper bound is the proportion of
/// Opaque effects. This is computable before any soak run.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FrozenRateUpperBound {
    pub opaque_effects: usize,
    pub total_effects: usize,
}

impl FrozenRateUpperBound {
    pub fn of(classes: &[EffectRecoveryClass]) -> Self {
        let opaque = classes
            .iter()
            .filter(|class| matches!(class, EffectRecoveryClass::Opaque))
            .count();
        Self {
            opaque_effects: opaque,
            total_effects: classes.len(),
        }
    }

    pub fn ratio(&self) -> f64 {
        if self.total_effects == 0 {
            0.0
        } else {
            self.opaque_effects as f64 / self.total_effects as f64
        }
    }
}

#[cfg(test)]
mod effect_recovery_tests {
    use super::*;

    #[test]
    fn recovery_procedure_is_derived_per_class() {
        assert_eq!(EffectRecoveryClass::Pure.crash_safe_action(), CrashSafeAction::Rerun);
        assert_eq!(
            EffectRecoveryClass::Idempotent { key: "k".into() }.crash_safe_action(),
            CrashSafeAction::RerunWithKey { key: "k".into() }
        );
        assert_eq!(
            EffectRecoveryClass::Queryable { probe: "rev-parse".into() }.crash_safe_action(),
            CrashSafeAction::ProbeThenResume { probe: "rev-parse".into() }
        );
        assert_eq!(EffectRecoveryClass::Opaque.crash_safe_action(), CrashSafeAction::Frozen);
    }

    #[test]
    fn only_opaque_requires_human() {
        assert!(EffectRecoveryClass::Pure.unattended_safe());
        assert!(EffectRecoveryClass::Idempotent { key: "k".into() }.unattended_safe());
        assert!(EffectRecoveryClass::Queryable { probe: "p".into() }.unattended_safe());
        assert!(!EffectRecoveryClass::Opaque.unattended_safe());
    }

    #[test]
    fn opaque_compensation_is_not_a_compensation() {
        assert!(EffectRecoveryClass::Idempotent { key: "k".into() }.compensable());
        assert!(EffectRecoveryClass::Queryable { probe: "p".into() }.compensable());
        assert!(!EffectRecoveryClass::Pure.compensable());
        assert!(!EffectRecoveryClass::Opaque.compensable());
    }

    #[test]
    fn frozen_rate_upper_bound_equals_opaque_proportion() {
        let classes = vec![
            EffectRecoveryClass::Pure,
            EffectRecoveryClass::Idempotent { key: "k".into() },
            EffectRecoveryClass::Queryable { probe: "p".into() },
            EffectRecoveryClass::Opaque,
            EffectRecoveryClass::Opaque,
        ];
        let bound = FrozenRateUpperBound::of(&classes);
        assert_eq!(bound.opaque_effects, 2);
        assert_eq!(bound.total_effects, 5);
        assert!((bound.ratio() - 0.4).abs() < 1e-9);
    }

    #[test]
    fn class_hash_distinguishes_key_and_probe() {
        let a = EffectRecoveryClass::Idempotent { key: "k1".into() };
        let b = EffectRecoveryClass::Idempotent { key: "k2".into() };
        assert_ne!(a.class_hash().unwrap(), b.class_hash().unwrap());
        let q1 = EffectRecoveryClass::Queryable { probe: "rev-parse".into() };
        let q2 = EffectRecoveryClass::Queryable { probe: "diff".into() };
        assert_ne!(q1.class_hash().unwrap(), q2.class_hash().unwrap());
        assert_ne!(a.class_hash().unwrap(), q1.class_hash().unwrap());
    }
}
