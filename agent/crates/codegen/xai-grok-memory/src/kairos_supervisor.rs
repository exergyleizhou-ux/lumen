//! S12 / NG-08 — KairosSupervisor pure control plane (local proof only).
//!
//! Built on SupervisorLoop + operation identity. Does **not** claim 24h
//! autonomy, auto-retry of Opaque effects, or a second agent runtime.

use serde::{Deserialize, Serialize};

use crate::evidence_loop::{
    LoopEffect, LoopPhase, SupervisorLoopEvent, SupervisorLoopState, reduce_supervisor_loop,
};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum KairosCommand {
    Claim,
    Heartbeat,
    Complete,
    Fail,
    Freeze,
    TakeOver,
    Cancel,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct KairosSupervisorState {
    pub id: String,
    pub loop_state: SupervisorLoopState,
    pub claimed_tree_id: Option<String>,
    pub last_command: Option<String>,
}

impl KairosSupervisorState {
    pub fn new(id: impl Into<String>) -> Self {
        let id = id.into();
        Self {
            loop_state: SupervisorLoopState::fresh(id.clone()),
            id,
            claimed_tree_id: None,
            last_command: None,
        }
    }

    pub fn phase(&self) -> LoopPhase {
        self.loop_state.phase
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum KairosDeny {
    Frozen,
    Cancelled,
    Terminal,
    NoClaim,
    StaleEpoch,
    ExternalEffectUnknown,
}

impl KairosDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::Frozen => "kairos.frozen",
            Self::Cancelled => "kairos.cancelled",
            Self::Terminal => "kairos.terminal",
            Self::NoClaim => "kairos.no_claim",
            Self::StaleEpoch => "kairos.stale_epoch",
            Self::ExternalEffectUnknown => "kairos.external_effect_unknown",
        }
    }
}

/// Apply an operator/API command. Pure: no IO, no second runtime.
pub fn apply_kairos_command(
    mut state: KairosSupervisorState,
    cmd: KairosCommand,
    tree_id: Option<&str>,
    lease_epoch: Option<u64>,
) -> Result<(KairosSupervisorState, LoopEffect), KairosDeny> {
    if matches!(
        state.phase(),
        LoopPhase::TerminalSucceeded | LoopPhase::TerminalFailed
    ) {
        return Err(KairosDeny::Terminal);
    }
    if matches!(state.phase(), LoopPhase::Cancelled) && !matches!(cmd, KairosCommand::Claim) {
        return Err(KairosDeny::Cancelled);
    }
    if matches!(state.phase(), LoopPhase::Frozen)
        && !matches!(cmd, KairosCommand::TakeOver | KairosCommand::Cancel)
    {
        return Err(KairosDeny::Frozen);
    }

    let event = match cmd {
        KairosCommand::Claim => {
            let epoch = lease_epoch.unwrap_or(state.loop_state.lease_epoch.saturating_add(1));
            state.claimed_tree_id = tree_id.map(str::to_owned);
            SupervisorLoopEvent::LeaseAcquired { epoch }
        }
        KairosCommand::Heartbeat => {
            if state.claimed_tree_id.is_none() {
                return Err(KairosDeny::NoClaim);
            }
            SupervisorLoopEvent::HeartbeatOk
        }
        KairosCommand::Complete | KairosCommand::Fail => {
            if state.claimed_tree_id.is_none() {
                return Err(KairosDeny::NoClaim);
            }
            // Completing a tree under supervision maps to child terminal.
            state.claimed_tree_id = None;
            SupervisorLoopEvent::HeartbeatOk
        }
        KairosCommand::Freeze => SupervisorLoopEvent::OperatorFreeze,
        KairosCommand::TakeOver => {
            let epoch = lease_epoch.unwrap_or(state.loop_state.lease_epoch.saturating_add(1));
            if epoch < state.loop_state.lease_epoch {
                return Err(KairosDeny::StaleEpoch);
            }
            // Frozen is terminal for auto-dispatch, but operator TakeOver is an
            // explicit re-entry: clear freeze before acquiring a new epoch.
            if matches!(state.loop_state.phase, LoopPhase::Frozen) {
                state.loop_state.phase = LoopPhase::Running;
            }
            state.claimed_tree_id = tree_id.map(str::to_owned);
            SupervisorLoopEvent::LeaseAcquired { epoch }
        }
        KairosCommand::Cancel => SupervisorLoopEvent::OperatorCancel,
    };

    let (loop_state, effect) = reduce_supervisor_loop(state.loop_state, event);
    state.loop_state = loop_state;
    state.last_command = Some(format!("{cmd:?}").to_ascii_lowercase());
    // Complete/Fail leave Running after heartbeat-ok; mark checkpointed if unclaimed.
    if matches!(cmd, KairosCommand::Complete | KairosCommand::Fail)
        && state.claimed_tree_id.is_none()
        && !state.phase().is_terminal()
    {
        state.loop_state.phase = LoopPhase::Checkpointed;
    }
    Ok((state, effect))
}

/// External effect unknown while holding a claim: always freeze (INV-15).
pub fn note_external_effect_unknown(
    state: KairosSupervisorState,
) -> (KairosSupervisorState, LoopEffect) {
    let (loop_state, effect) =
        reduce_supervisor_loop(state.loop_state, SupervisorLoopEvent::ExternalEffectUnknown);
    (
        KairosSupervisorState {
            loop_state,
            ..state
        },
        effect,
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn claim_heartbeat_complete_and_freeze_paths() {
        let s = KairosSupervisorState::new("k1");
        let (s, _) = apply_kairos_command(s, KairosCommand::Claim, Some("tree"), Some(1)).unwrap();
        assert_eq!(s.claimed_tree_id.as_deref(), Some("tree"));
        let (s, _) = apply_kairos_command(s, KairosCommand::Heartbeat, None, None).unwrap();
        assert_eq!(s.phase(), LoopPhase::Running);
        let (s, _) = apply_kairos_command(s, KairosCommand::Complete, None, None).unwrap();
        assert_eq!(s.phase(), LoopPhase::Checkpointed);

        let s = KairosSupervisorState::new("k2");
        let (s, _) = apply_kairos_command(s, KairosCommand::Claim, Some("t"), Some(1)).unwrap();
        let (s, effect) = apply_kairos_command(s, KairosCommand::Freeze, None, None).unwrap();
        assert_eq!(s.phase(), LoopPhase::Frozen);
        assert!(matches!(effect, LoopEffect::MarkFrozen { .. }));
        assert!(
            apply_kairos_command(s.clone(), KairosCommand::Heartbeat, None, None).is_err()
        );
        let (s, _) = apply_kairos_command(s, KairosCommand::TakeOver, Some("t"), Some(2)).unwrap();
        assert_eq!(s.phase(), LoopPhase::Running);
    }

    #[test]
    fn external_effect_unknown_never_auto_retries() {
        let s = KairosSupervisorState::new("k3");
        let (s, _) = apply_kairos_command(s, KairosCommand::Claim, Some("t"), Some(1)).unwrap();
        let (s, effect) = note_external_effect_unknown(s);
        assert_eq!(s.phase(), LoopPhase::Frozen);
        assert!(matches!(
            effect,
            LoopEffect::MarkFrozen { reason } if reason == "external_effect_unknown"
        ));
    }
}
