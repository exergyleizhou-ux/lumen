//! Activity read model used by the actor-owned idle-unload decision.
//!
//! A session turn being idle is not enough to make its actor disposable: an
//! owned background terminal/monitor, direct child, scheduler run lease, or
//! parked interaction still needs the resident authority that owns its
//! lifecycle.  This module deliberately reports only current runtime facts;
//! it does not turn an ordinary future cron into live work.

use super::*;

/// Bound the external activity reads performed by [`SessionCommand::UnloadIfIdle`].
///
/// The actor mailbox is intentionally held while these adapters are queried so
/// a prompt already queued before `UnloadIfIdle` is still ordered before the
/// unload decision.  A stalled terminal/coordinator/scheduler must therefore
/// fail closed rather than pin the actor loop indefinitely or allow an
/// unobserved process to outlive its owner.
pub(super) const IDLE_ACTIVITY_PROBE_TIMEOUT: std::time::Duration =
    std::time::Duration::from_millis(250);

/// Non-secret, current activity which makes an idle actor ineligible for
/// unload.  This is intentionally a read model rather than a second lifecycle
/// authority: `SessionActor` remains the only component that can accept
/// shutdown.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub(super) struct SessionActivitySnapshot {
    pub(super) foreground_or_queued: bool,
    pub(super) pending_interactions: usize,
    pub(super) background_terminals: usize,
    pub(super) monitors: usize,
    pub(super) direct_subagents: usize,
    pub(super) scheduler_run_leases: usize,
    pub(super) probe_timed_out: bool,
}

impl SessionActivitySnapshot {
    /// Only an entirely empty, successfully observed snapshot is unloadable.
    pub(super) const fn blocks_idle_unload(self) -> bool {
        self.foreground_or_queued
            || self.pending_interactions > 0
            || self.background_terminals > 0
            || self.monitors > 0
            || self.direct_subagents > 0
            || self.scheduler_run_leases > 0
            || self.probe_timed_out
    }

    pub(super) const fn timed_out() -> Self {
        Self {
            probe_timed_out: true,
            ..Self::empty()
        }
    }

    const fn empty() -> Self {
        Self {
            foreground_or_queued: false,
            pending_interactions: 0,
            background_terminals: 0,
            monitors: 0,
            direct_subagents: 0,
            scheduler_run_leases: 0,
            probe_timed_out: false,
        }
    }
}

/// Whether a task belongs to `session_id` on a backend shared with children.
///
/// Older backends did not attach an owner.  Treating that legacy shape as local
/// is conservative for unload and preserves existing single-session behavior;
/// owner-aware backends must never make one session wait for a sibling's task.
pub(super) fn task_is_owned_by_session(
    task: &xai_grok_tools::computer::types::TaskSnapshot,
    session_id: &str,
) -> bool {
    task.owner_session_id
        .as_deref()
        .is_none_or(|owner| owner == session_id)
}

fn owned_outstanding_task_counts(
    session_id: &str,
    tasks: &[xai_grok_tools::computer::types::TaskSnapshot],
) -> (usize, usize) {
    tasks
        .iter()
        .filter(|task| task.is_outstanding() && task_is_owned_by_session(task, session_id))
        .fold((0, 0), |(terminals, monitors), task| match task.kind {
            xai_grok_tools::computer::types::TaskKind::Bash => (terminals + 1, monitors),
            xai_grok_tools::computer::types::TaskKind::Monitor => (terminals, monitors + 1),
        })
}

impl SessionActor {
    /// Snapshot all currently-known activity which must keep this actor
    /// resident.  The caller bounds this method with
    /// [`IDLE_ACTIVITY_PROBE_TIMEOUT`], so any unavailable external adapter is
    /// an explicit deny rather than a partial "idle" answer.
    pub(super) async fn idle_unload_activity_snapshot(&self) -> SessionActivitySnapshot {
        let foreground_or_queued = {
            let state = self.state.lock().await;
            state_is_busy(&state)
        };
        if foreground_or_queued {
            return SessionActivitySnapshot {
                foreground_or_queued: true,
                ..SessionActivitySnapshot::empty()
            };
        }

        let pending_interactions = self
            .pending_interactions
            .lock()
            .map(|interactions| interactions.len())
            // A poisoned lock must not be interpreted as permission to tear
            // down an actor with an unknown blocked interaction.
            .unwrap_or(usize::MAX);
        if pending_interactions > 0 {
            return SessionActivitySnapshot {
                pending_interactions,
                ..SessionActivitySnapshot::empty()
            };
        }

        let bridge = self.tool_bridge_handle();
        let scheduler_bridge = bridge.clone();
        let (tasks, direct_subagents, scheduled_tasks) = tokio::join!(
            bridge.list_background_tasks(),
            self.list_active_subagents(),
            scheduler_bridge.list_scheduled_tasks(),
        );
        let (background_terminals, monitors) =
            owned_outstanding_task_counts(&self.session_id_string(), &tasks);
        let scheduler_run_leases = scheduled_tasks
            .iter()
            .filter(|task| task.has_active_run_lease())
            .count();

        SessionActivitySnapshot {
            foreground_or_queued: false,
            pending_interactions: 0,
            background_terminals,
            monitors,
            direct_subagents: direct_subagents.len(),
            scheduler_run_leases,
            probe_timed_out: false,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use xai_grok_tools::computer::types::{TaskKind, TaskSnapshot};

    fn task(owner: Option<&str>, kind: TaskKind, completed: bool) -> TaskSnapshot {
        TaskSnapshot {
            task_id: "task".to_owned(),
            command: "sleep 60".to_owned(),
            display_command: None,
            cwd: "/tmp".to_owned(),
            start_time: std::time::SystemTime::now(),
            end_time: completed.then(std::time::SystemTime::now),
            output: String::new(),
            output_file: std::path::PathBuf::new(),
            truncated: false,
            exit_code: completed.then_some(0),
            signal: None,
            completed,
            kind,
            block_waited: false,
            explicitly_killed: false,
            owner_session_id: owner.map(str::to_owned),
            description: None,
            is_backgrounded: true,
        }
    }

    #[test]
    fn owned_outstanding_task_counts_excludes_foreign_and_completed_tasks() {
        let tasks = vec![
            task(Some("session-a"), TaskKind::Bash, false),
            task(Some("session-a"), TaskKind::Monitor, false),
            task(Some("session-b"), TaskKind::Bash, false),
            task(Some("session-a"), TaskKind::Monitor, true),
            // An owner-less legacy backend is conservatively treated as local.
            task(None, TaskKind::Bash, false),
        ];

        assert_eq!(owned_outstanding_task_counts("session-a", &tasks), (2, 1));
    }

    #[test]
    fn every_activity_dimension_blocks_idle_unload() {
        assert!(!SessionActivitySnapshot::default().blocks_idle_unload());
        for snapshot in [
            SessionActivitySnapshot {
                foreground_or_queued: true,
                ..Default::default()
            },
            SessionActivitySnapshot {
                pending_interactions: 1,
                ..Default::default()
            },
            SessionActivitySnapshot {
                background_terminals: 1,
                ..Default::default()
            },
            SessionActivitySnapshot {
                monitors: 1,
                ..Default::default()
            },
            SessionActivitySnapshot {
                direct_subagents: 1,
                ..Default::default()
            },
            SessionActivitySnapshot {
                scheduler_run_leases: 1,
                ..Default::default()
            },
            SessionActivitySnapshot::timed_out(),
        ] {
            assert!(snapshot.blocks_idle_unload(), "{snapshot:?}");
        }
    }
}
