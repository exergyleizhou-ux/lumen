use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use tokio::sync::{mpsc, oneshot};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct SchedulerVersion {
    generation: uuid::Uuid,
    revision: u64,
}

impl SchedulerVersion {
    pub(super) fn generation(self) -> String {
        self.generation.to_string()
    }

    pub(super) fn revision(self) -> u64 {
        self.revision
    }

    pub(super) fn generation_id(self) -> uuid::Uuid {
        self.generation
    }

    #[cfg(test)]
    pub(super) fn from_parts(generation: uuid::Uuid, revision: u64) -> Self {
        Self {
            generation,
            revision,
        }
    }
}

#[derive(Debug)]
pub(crate) struct SchedulerClock {
    version: SchedulerVersion,
    /// Identifies this in-memory SchedulerActor lifetime, independently from
    /// the externally visible version generation that can roll over at
    /// `u64::MAX`.
    run_lease_owner: uuid::Uuid,
}

#[derive(Debug)]
pub(crate) struct SchedulerReservation {
    source: SchedulerVersion,
    generation: uuid::Uuid,
    next_revision: u64,
    remaining: u64,
    rollover: Option<GenerationRollover>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) struct GenerationRollover {
    pub(crate) old_generation: uuid::Uuid,
    pub(crate) new_generation: uuid::Uuid,
}

pub(crate) struct SchedulerCommit {
    pub(crate) version: SchedulerVersion,
    pub(crate) rollover: Option<GenerationRollover>,
}

impl SchedulerReservation {
    pub(crate) fn version_at(&self, offset: u64) -> SchedulerVersion {
        assert!(
            offset < self.remaining,
            "scheduler reservation offset is exhausted"
        );
        SchedulerVersion {
            generation: self.generation,
            revision: self.next_revision + offset,
        }
    }

    pub(crate) fn commit_next(&mut self, clock: &mut SchedulerClock) -> SchedulerCommit {
        assert!(self.remaining > 0, "scheduler reservation is exhausted");
        let rollover = self.rollover;
        let expected_source = rollover.map_or(
            SchedulerVersion {
                generation: self.generation,
                revision: self.next_revision - 1,
            },
            |_| self.source,
        );
        assert_eq!(
            clock.version, expected_source,
            "stale scheduler reservation"
        );

        let version = self.version_at(0);
        clock.version = version;
        self.rollover = None;
        self.remaining -= 1;
        if self.remaining > 0 {
            self.next_revision = self
                .next_revision
                .checked_add(1)
                .expect("preflighted revision");
        }
        SchedulerCommit { version, rollover }
    }
}

impl SchedulerClock {
    pub(crate) fn new() -> Self {
        Self {
            version: SchedulerVersion {
                generation: uuid::Uuid::now_v7(),
                revision: 0,
            },
            run_lease_owner: uuid::Uuid::now_v7(),
        }
    }

    pub(crate) fn snapshot(&self) -> SchedulerVersion {
        self.version
    }

    pub(crate) fn run_lease_owner_id(&self) -> String {
        format!("scheduler:{}", self.run_lease_owner)
    }

    pub(crate) fn prepare_transition(&self, count: usize) -> SchedulerReservation {
        assert!(
            count > 0 && count <= MAX_SCHEDULER_TRANSITIONS,
            "invalid scheduler reservation size"
        );
        let count = count as u64;
        let rollover =
            self.version
                .revision
                .checked_add(count)
                .is_none()
                .then(|| GenerationRollover {
                    old_generation: self.version.generation,
                    new_generation: uuid::Uuid::now_v7(),
                });
        SchedulerReservation {
            source: self.version,
            generation: rollover
                .map(|rollover| rollover.new_generation)
                .unwrap_or(self.version.generation),
            next_revision: if rollover.is_some() {
                1
            } else {
                self.version.revision + 1
            },
            remaining: count,
            rollover,
        }
    }

    #[cfg(test)]
    pub(super) fn at_revision_for_test(revision: u64) -> Self {
        let mut clock = Self::new();
        clock.version.revision = revision;
        clock
    }
}

impl Default for SchedulerClock {
    fn default() -> Self {
        Self::new()
    }
}

#[derive(thiserror::Error, Debug)]
pub enum SchedulerError {
    #[error("invalid interval: {0}")]
    InvalidInterval(String),

    #[error("invalid scheduled task: {0}")]
    InvalidTask(String),

    #[error("maximum of {0} scheduled tasks reached")]
    TaskLimitReached(usize),

    #[error("no scheduled task with id {0}; call scheduler_list to see active task ids")]
    TaskNotFound(String),

    #[error("failed to persist scheduler resources: {0}")]
    Persistence(#[source] std::io::Error),

    #[error("failed to publish scheduler tombstone: {0}")]
    Notification(#[source] crate::notification::NotificationAcknowledgementError),

    #[error("durable scheduler removal requires an acknowledging notification consumer")]
    NoDurableNotificationConsumer,

    #[error("scheduler removal for {0} is pending")]
    RemovalPending(String),

    #[error("scheduler removal cancelled")]
    Cancelled,

    #[error("scheduler removal timed out")]
    Timeout,
}

pub fn scheduler_tool_error(error: SchedulerError) -> xai_tool_runtime::ToolError {
    let code = match &error {
        SchedulerError::InvalidInterval(_)
        | SchedulerError::InvalidTask(_)
        | SchedulerError::TaskLimitReached(_)
        | SchedulerError::TaskNotFound(_) => "scheduler_invalid_request",
        SchedulerError::Persistence(_) => "scheduler_persistence",
        SchedulerError::Notification(_) => "scheduler_notification",
        SchedulerError::NoDurableNotificationConsumer => "scheduler_durability_unavailable",
        SchedulerError::RemovalPending(_) => "scheduler_removal_pending",
        SchedulerError::Cancelled => "scheduler_cancelled",
        SchedulerError::Timeout => "scheduler_timeout",
    };
    xai_tool_runtime::ToolError::custom(code, error.to_string())
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ScheduledTask {
    pub id: String,
    pub interval_secs: u64,
    pub prompt: String,
    #[serde(default = "default_recurring")]
    pub recurring: bool,
    #[serde(default)]
    pub durable: bool,
    #[serde(default)]
    pub foreground: bool,
    pub created_at: DateTime<Utc>,
    pub last_fired_at: Option<DateTime<Utc>>,
    pub expires_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub last_subagent_id: Option<String>,
    #[serde(default)]
    pub iterations_since_fresh: u32,
    /// Set when the prompt is patched: the next fire starts a fresh
    /// transcript instead of resuming the old task's. The anchor itself is
    /// kept until then so the in-flight guard can still see a running
    /// iteration.
    #[serde(default)]
    pub chain_reset_pending: bool,
    /// Durable ownership of an in-flight background iteration.  A restored
    /// scheduler must treat an unexpired lease as running work rather than
    /// spawning a second copy after a process restart.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub(crate) active_run_lease: Option<SchedulerRunLease>,
    /// Durable audit receipt emitted when an expired lease is replaced by a
    /// new actor.  This is distinct from ordinary acquire/release events so
    /// recovery tooling can surface an autonomous takeover for review.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub(crate) last_run_lease_takeover: Option<SchedulerRunLeaseTakeover>,
    /// Idempotency-oriented terminal receipt for the most recent background
    /// run. It deliberately excludes model output and error text.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub(crate) last_run_receipt: Option<SchedulerRunReceipt>,
    /// Consecutive terminal failures of background iterations. A successful
    /// run resets this counter; cancellation is not retried automatically.
    #[serde(default)]
    pub(crate) consecutive_run_failures: u32,
    /// Do not dispatch before this wall-clock time after a retryable failure.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub(crate) retry_not_before: Option<DateTime<Utc>>,
    /// A bounded failure budget has been exhausted. The task stays visible for
    /// inspection but is no longer eligible for autonomous dispatch.
    #[serde(default)]
    pub(crate) dead_lettered: bool,
    /// A completed background run reported incomplete token usage. Autonomous
    /// recurrence stops until a user updates the task, because cost/budget
    /// enforcement can no longer be proven.
    #[serde(default)]
    pub(crate) usage_verification_required: bool,
    /// Optional UTC-day token ceiling for a background scheduler task. The
    /// verified usage accumulator is persisted rather than inferred from a
    /// lossy last-run receipt.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub(crate) daily_token_budget: Option<u64>,
    /// UTC day represented by `daily_tokens_used`.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub(crate) daily_token_usage_day: Option<chrono::NaiveDate>,
    /// Provider-reported, complete token usage for `daily_token_usage_day`.
    #[serde(default)]
    pub(crate) daily_tokens_used: u64,
}

/// A bounded, persisted claim to execute one scheduler task.
///
/// It deliberately carries no process handle or credentials: those stay in
/// the SessionActor/coordinator.  This record only establishes the durable
/// ownership fence used before a background process is created or resumed.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct SchedulerRunLease {
    owner_id: String,
    acquired_at: DateTime<Utc>,
    heartbeat_at: DateTime<Utc>,
    expires_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct SchedulerRunLeaseTakeover {
    previous_owner_id: String,
    previous_heartbeat_at: DateTime<Utc>,
    taken_at: DateTime<Utc>,
}

impl SchedulerRunLeaseTakeover {
    /// The only takeover detail safe to expose in the task read model.  The
    /// previous owner identifier remains an internal fencing detail.
    pub(crate) fn taken_at(&self) -> DateTime<Utc> {
        self.taken_at
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct SchedulerRunReceipt {
    run_id: String,
    status: SchedulerRunStatus,
    completed_at: DateTime<Utc>,
    duration_ms: u64,
    total_tokens_used: u64,
    /// Model resolved for the child at execution start.  Older receipts did
    /// not record this, so absence is preserved as unknown rather than
    /// retroactively guessed from current configuration.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    model_id: Option<String>,
    /// `true` means the token total is not safe to use as a budget or cost
    /// proof.  Keep this durable instead of silently presenting zero/partial
    /// usage as a complete background-run receipt.
    #[serde(default)]
    output_usage_incomplete: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub(crate) enum SchedulerRunStatus {
    Completed,
    Failed,
    Cancelled,
}

impl SchedulerRunReceipt {
    pub(crate) fn new(
        run_id: String,
        status: SchedulerRunStatus,
        completed_at: DateTime<Utc>,
        duration_ms: u64,
        total_tokens_used: u64,
        model_id: Option<String>,
        output_usage_incomplete: bool,
    ) -> Self {
        Self {
            run_id,
            status,
            completed_at,
            duration_ms,
            total_tokens_used,
            model_id,
            output_usage_incomplete,
        }
    }

    #[cfg(test)]
    pub(crate) fn run_id(&self) -> &str {
        &self.run_id
    }

    pub(crate) fn status(&self) -> SchedulerRunStatus {
        self.status
    }

    pub(crate) fn output_usage_incomplete(&self) -> bool {
        self.output_usage_incomplete
    }

    pub(crate) fn model_id(&self) -> Option<&str> {
        self.model_id.as_deref()
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum SchedulerLeaseAcquisition {
    Fresh,
    ReplacedExpiredLease,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum SchedulerLeaseError {
    InvalidOwner,
    InvalidTtl,
    HeldByActiveOwner,
    NotHeld,
    OwnerMismatch,
    Expired,
}

impl SchedulerRunLease {
    pub(crate) fn owner_id(&self) -> &str {
        &self.owner_id
    }

    pub(crate) fn is_expired(&self, now: DateTime<Utc>) -> bool {
        now >= self.expires_at
    }
}

pub const LOOP_FRESH_CHAIN_EVERY: u32 = 10;

pub const LOOP_COMPLETION_OUTPUT_CAP: usize = 4_000;

pub const MAX_SCHEDULER_RUN_FAILURES: u32 = 5;

const MAX_SCHEDULER_TRANSITIONS: usize = 50;

fn default_recurring() -> bool {
    true
}

impl ScheduledTask {
    pub fn new(interval_secs: u64, prompt: String, recurring: bool, durable: bool) -> Self {
        Self::with_fire_immediately(interval_secs, prompt, recurring, durable, false)
    }

    pub fn with_fire_immediately(
        interval_secs: u64,
        prompt: String,
        recurring: bool,
        durable: bool,
        fire_immediately: bool,
    ) -> Self {
        let now = Utc::now();
        // When fire_immediately is true, anchor created_at in the past so that
        // next_fire_at() = created_at + interval = now, firing on the first tick.
        let created_at = if fire_immediately {
            now - chrono::Duration::seconds(interval_secs as i64)
        } else {
            now
        };
        Self {
            id: uuid::Uuid::now_v7().to_string().replace('-', "")[..12].to_string(),
            interval_secs,
            prompt,
            recurring,
            durable,
            foreground: false,
            created_at,
            last_fired_at: None,
            expires_at: if recurring {
                Some(now + chrono::Duration::days(7))
            } else {
                None
            },
            last_subagent_id: None,
            iterations_since_fresh: 0,
            chain_reset_pending: false,
            active_run_lease: None,
            last_run_lease_takeover: None,
            last_run_receipt: None,
            consecutive_run_failures: 0,
            retry_not_before: None,
            dead_lettered: false,
            usage_verification_required: false,
            daily_token_budget: None,
            daily_token_usage_day: None,
            daily_tokens_used: 0,
        }
    }

    pub(crate) fn acquire_run_lease(
        &mut self,
        owner_id: &str,
        now: DateTime<Utc>,
        ttl: chrono::Duration,
    ) -> Result<SchedulerLeaseAcquisition, SchedulerLeaseError> {
        validate_lease_request(owner_id, ttl)?;
        let acquisition = match self.active_run_lease.as_ref() {
            Some(lease) if !lease.is_expired(now) => {
                return Err(SchedulerLeaseError::HeldByActiveOwner);
            }
            Some(lease) => {
                self.last_run_lease_takeover = Some(SchedulerRunLeaseTakeover {
                    previous_owner_id: lease.owner_id.clone(),
                    previous_heartbeat_at: lease.heartbeat_at,
                    taken_at: now,
                });
                SchedulerLeaseAcquisition::ReplacedExpiredLease
            }
            None => SchedulerLeaseAcquisition::Fresh,
        };
        let expires_at = now
            .checked_add_signed(ttl)
            .ok_or(SchedulerLeaseError::InvalidTtl)?;
        self.active_run_lease = Some(SchedulerRunLease {
            owner_id: owner_id.to_owned(),
            acquired_at: now,
            heartbeat_at: now,
            expires_at,
        });
        Ok(acquisition)
    }

    pub(crate) fn renew_run_lease(
        &mut self,
        owner_id: &str,
        now: DateTime<Utc>,
        ttl: chrono::Duration,
    ) -> Result<(), SchedulerLeaseError> {
        validate_lease_request(owner_id, ttl)?;
        let lease = self
            .active_run_lease
            .as_mut()
            .ok_or(SchedulerLeaseError::NotHeld)?;
        if lease.owner_id != owner_id {
            return Err(SchedulerLeaseError::OwnerMismatch);
        }
        if lease.is_expired(now) {
            return Err(SchedulerLeaseError::Expired);
        }
        lease.heartbeat_at = now;
        lease.expires_at = now
            .checked_add_signed(ttl)
            .ok_or(SchedulerLeaseError::InvalidTtl)?;
        Ok(())
    }

    pub(crate) fn release_run_lease(&mut self, owner_id: &str) -> Result<(), SchedulerLeaseError> {
        let lease = self
            .active_run_lease
            .as_ref()
            .ok_or(SchedulerLeaseError::NotHeld)?;
        if lease.owner_id != owner_id {
            return Err(SchedulerLeaseError::OwnerMismatch);
        }
        self.active_run_lease = None;
        Ok(())
    }

    pub(crate) fn record_terminal_run_status(
        &mut self,
        status: SchedulerRunStatus,
        now: DateTime<Utc>,
    ) {
        match status {
            SchedulerRunStatus::Completed => {
                self.consecutive_run_failures = 0;
                self.retry_not_before = None;
                self.dead_lettered = false;
            }
            SchedulerRunStatus::Cancelled => {
                // A cancellation is an intentional stop signal, not an
                // autonomous retry request.
                self.retry_not_before = None;
            }
            SchedulerRunStatus::Failed => {
                self.consecutive_run_failures = self.consecutive_run_failures.saturating_add(1);
                if self.consecutive_run_failures >= MAX_SCHEDULER_RUN_FAILURES {
                    self.dead_lettered = true;
                    self.retry_not_before = None;
                    return;
                }
                let exponent = self.consecutive_run_failures.saturating_sub(1).min(6);
                let delay_secs = 5_u64.saturating_mul(1_u64 << exponent).min(300);
                self.retry_not_before = now.checked_add_signed(chrono::Duration::seconds(
                    i64::try_from(delay_secs).expect("bounded retry delay fits i64"),
                ));
            }
        }
    }

    pub(crate) fn clear_failure_backoff(&mut self) {
        self.consecutive_run_failures = 0;
        self.retry_not_before = None;
        self.dead_lettered = false;
        self.usage_verification_required = false;
    }

    pub(crate) fn set_daily_token_budget(
        &mut self,
        daily_token_budget: Option<u64>,
    ) -> Result<(), SchedulerError> {
        if matches!(daily_token_budget, Some(0)) {
            return Err(SchedulerError::InvalidTask(
                "daily_token_budget must be greater than zero".to_owned(),
            ));
        }
        self.daily_token_budget = daily_token_budget;
        Ok(())
    }

    pub(crate) fn record_verified_daily_token_usage(&mut self, now: DateTime<Utc>, used: u64) {
        let today = now.date_naive();
        if self.daily_token_usage_day != Some(today) {
            self.daily_token_usage_day = Some(today);
            self.daily_tokens_used = 0;
        }
        self.daily_tokens_used = self.daily_tokens_used.saturating_add(used);
    }

    pub(crate) fn daily_token_usage_for(&self, now: DateTime<Utc>) -> u64 {
        (self.daily_token_usage_day == Some(now.date_naive()))
            .then_some(self.daily_tokens_used)
            .unwrap_or(0)
    }

    pub(crate) fn daily_token_budget_exhausted(&self, now: DateTime<Utc>) -> bool {
        self.daily_token_budget
            .is_some_and(|budget| self.daily_token_usage_for(now) >= budget)
    }

    pub(crate) fn next_daily_budget_reset_at(&self, now: DateTime<Utc>) -> Option<DateTime<Utc>> {
        self.daily_token_budget_exhausted(now).then(|| {
            now.date_naive()
                .succ_opt()
                .expect("UTC date has a next day")
                .and_hms_opt(0, 0, 0)
                .expect("midnight is valid")
                .and_utc()
        })
    }

    /// Next fire time, computed from `last_fired_at` (or `created_at` if never fired).
    pub fn next_fire_at(&self) -> DateTime<Utc> {
        let anchor = self.last_fired_at.unwrap_or(self.created_at);
        anchor + chrono::Duration::seconds(self.interval_secs as i64)
    }

    /// The actual autonomous dispatch deadline, including retry backoff.
    pub fn next_due_at(&self) -> DateTime<Utc> {
        self.retry_not_before
            .map(|retry| self.next_fire_at().max(retry))
            .unwrap_or_else(|| self.next_fire_at())
    }

    /// The next time a scheduler actor may attempt dispatch. An active lease
    /// is a fence even when the task's cadence is already overdue; otherwise
    /// a recovered actor would spin on an unexpired foreign lease.
    pub fn next_dispatch_at(&self) -> DateTime<Utc> {
        self.active_run_lease
            .as_ref()
            .map(|lease| self.next_due_at().max(lease.expires_at))
            .unwrap_or_else(|| self.next_due_at())
    }

    pub(crate) fn next_dispatch_at_for_owner(&self, owner_id: &str) -> DateTime<Utc> {
        self.next_dispatch_at_for_owner_at(owner_id, Utc::now())
    }

    pub(crate) fn next_dispatch_at_for_owner_at(
        &self,
        owner_id: &str,
        now: DateTime<Utc>,
    ) -> DateTime<Utc> {
        let base = match self.active_run_lease.as_ref() {
            Some(lease) if lease.owner_id() != owner_id => self.next_due_at().max(lease.expires_at),
            _ => self.next_due_at(),
        };
        self.next_daily_budget_reset_at(now)
            .map_or(base, |reset| base.max(reset))
    }

    #[cfg(test)]
    pub(crate) fn is_dispatchable(&self, now: DateTime<Utc>) -> bool {
        !self.dead_lettered
            && !self.usage_verification_required
            && !self.daily_token_budget_exhausted(now)
            && self.next_dispatch_at() <= now
    }

    pub(crate) fn is_dispatchable_for_owner(&self, owner_id: &str, now: DateTime<Utc>) -> bool {
        !self.dead_lettered
            && !self.usage_verification_required
            && !self.daily_token_budget_exhausted(now)
            && self.next_dispatch_at_for_owner(owner_id) <= now
    }

    /// Whether this task has expired (recurring tasks only).
    pub fn is_expired(&self, now: DateTime<Utc>) -> bool {
        self.expires_at.is_some_and(|exp| now >= exp)
    }
}

fn validate_lease_request(
    owner_id: &str,
    ttl: chrono::Duration,
) -> Result<(), SchedulerLeaseError> {
    if owner_id.trim().is_empty() || owner_id.len() > 256 {
        return Err(SchedulerLeaseError::InvalidOwner);
    }
    if ttl <= chrono::Duration::zero() || ttl > chrono::Duration::minutes(10) {
        return Err(SchedulerLeaseError::InvalidTtl);
    }
    Ok(())
}

/// Persisted state for the scheduler, stored via Resources + ResourcesPersistence.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct SchedulerState {
    #[serde(default)]
    pub tasks: Vec<ScheduledTask>,
    #[serde(
        default,
        rename = "occurrenceJournal",
        skip_serializing_if = "super::occurrence_journal::OccurrenceJournal::is_empty"
    )]
    pub(crate) occurrence_journal: super::occurrence_journal::OccurrenceJournal,
}

crate::register_resource!("grok_build", "Scheduler", SchedulerState);

#[derive(Debug, Clone)]
pub struct SchedulerSnapshot {
    // Consumed by the authoritative scheduler snapshot layer in the next migration PR.
    #[allow(dead_code)]
    pub(crate) version: SchedulerVersion,
    pub tasks: Vec<ScheduledTask>,
    /// Durable one-shots are blocked pending root recovery. Recurring tasks
    /// can still be listed, but an automation host must not treat the
    /// scheduler as healthy for autonomous one-shot dispatch.
    pub recovery_required: bool,
    /// Number of quarantined durable one-shot task ids. IDs are intentionally
    /// not surfaced through the generic read-model.
    pub quarantined_one_shot_count: usize,
}

impl SchedulerState {
    pub(crate) fn recovery_status(&self) -> (bool, usize) {
        self.occurrence_journal.recovery_status()
    }
}

/// Handle for tools to communicate with the SchedulerActor.
/// Ephemeral -- not serialized, not persisted. Inserted via `resources.insert()`.
#[derive(Clone)]
pub struct SchedulerHandle(pub mpsc::UnboundedSender<SchedulerCommand>);

pub enum SchedulerCommand {
    Create {
        task: ScheduledTask,
        reply: oneshot::Sender<Result<ScheduledTask, SchedulerError>>,
    },
    Update {
        id: String,
        prompt: Option<String>,
        interval_secs: Option<u64>,
        reply: oneshot::Sender<Result<ScheduledTask, SchedulerError>>,
    },
    UpdateDailyTokenBudget {
        id: String,
        daily_token_budget: Option<u64>,
        reply: oneshot::Sender<Result<ScheduledTask, SchedulerError>>,
    },
    Delete {
        id: String,
        reply: oneshot::Sender<Result<bool, SchedulerError>>,
    },
    List {
        reply: oneshot::Sender<SchedulerSnapshot>,
    },
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn new_recurring_task_has_7_day_expiry() {
        let task = ScheduledTask::new(300, "check deploy".into(), true, false);
        assert!(task.expires_at.is_some());
        let expiry = task.expires_at.unwrap();
        let diff = expiry - task.created_at;
        assert_eq!(diff.num_days(), 7);
    }

    #[test]
    fn new_one_shot_task_has_no_expiry() {
        let task = ScheduledTask::new(300, "check deploy".into(), false, false);
        assert!(task.expires_at.is_none());
    }

    #[test]
    fn daily_token_budget_blocks_only_its_utc_day() {
        let now = Utc::now();
        let mut task = ScheduledTask::new(1, "budgeted".into(), true, true);
        task.last_fired_at = Some(now - chrono::Duration::seconds(2));
        task.set_daily_token_budget(Some(100)).unwrap();
        task.record_verified_daily_token_usage(now, 100);

        assert!(task.daily_token_budget_exhausted(now));
        assert!(!task.is_dispatchable(now));
        assert_eq!(
            task.next_dispatch_at_for_owner_at("scheduler", now),
            now.date_naive()
                .succ_opt()
                .unwrap()
                .and_hms_opt(0, 0, 0)
                .unwrap()
                .and_utc()
        );

        let tomorrow = now + chrono::Duration::days(1);
        assert_eq!(task.daily_token_usage_for(tomorrow), 0);
        assert!(!task.daily_token_budget_exhausted(tomorrow));
        assert!(task.is_dispatchable(tomorrow));
    }

    #[test]
    fn zero_daily_token_budget_is_rejected() {
        let mut task = ScheduledTask::new(1, "budgeted".into(), true, true);
        let error = task.set_daily_token_budget(Some(0)).unwrap_err();
        assert!(error.to_string().contains("greater than zero"));
        assert!(task.daily_token_budget.is_none());
    }

    #[test]
    fn next_fire_at_uses_created_at_when_never_fired() {
        let task = ScheduledTask::new(300, "test".into(), true, false);
        let expected = task.created_at + chrono::Duration::seconds(300);
        assert_eq!(task.next_fire_at(), expected);
    }

    #[test]
    fn next_fire_at_uses_last_fired_at_when_present() {
        let mut task = ScheduledTask::new(300, "test".into(), true, false);
        let fired = Utc::now();
        task.last_fired_at = Some(fired);
        let expected = fired + chrono::Duration::seconds(300);
        assert_eq!(task.next_fire_at(), expected);
    }

    #[test]
    fn is_expired_returns_true_when_past_expiry() {
        let mut task = ScheduledTask::new(300, "test".into(), true, false);
        task.expires_at = Some(Utc::now() - chrono::Duration::hours(1));
        assert!(task.is_expired(Utc::now()));
    }

    #[test]
    fn is_expired_returns_false_when_before_expiry() {
        let task = ScheduledTask::new(300, "test".into(), true, false);
        assert!(!task.is_expired(Utc::now()));
    }

    #[test]
    fn is_expired_returns_false_for_one_shot() {
        let task = ScheduledTask::new(300, "test".into(), false, false);
        assert!(!task.is_expired(Utc::now()));
    }

    #[test]
    fn legacy_state_defaults_recurring_and_durable_fields() {
        let json = r#"{"id":"abc123","intervalSecs":300,"prompt":"check",
                       "createdAt":"2026-01-01T00:00:00Z",
                       "lastFiredAt":null,"expiresAt":null}"#;
        let task: ScheduledTask = serde_json::from_str(json).unwrap();
        assert!(task.recurring && !task.durable);
        assert!(task.active_run_lease.is_none());
        assert!(task.last_run_lease_takeover.is_none());
        assert!(task.last_run_receipt.is_none());
        assert_eq!(task.consecutive_run_failures, 0);
        assert!(task.retry_not_before.is_none());
        assert!(!task.dead_lettered);
    }

    #[test]
    fn legacy_run_receipt_keeps_model_identity_unknown() {
        let receipt: SchedulerRunReceipt = serde_json::from_str(
            r#"{"runId":"run-1","status":"completed","completedAt":"2026-01-01T00:00:00Z","durationMs":7,"totalTokensUsed":11,"outputUsageIncomplete":false}"#,
        )
        .unwrap();
        assert_eq!(receipt.model_id(), None);
    }

    #[test]
    fn run_lease_is_owner_fenced_and_requires_heartbeat_before_expiry() {
        let now = Utc::now();
        let ttl = chrono::Duration::seconds(30);
        let mut task = ScheduledTask::new(300, "test".into(), true, true);

        assert_eq!(
            task.acquire_run_lease("scheduler-a", now, ttl).unwrap(),
            SchedulerLeaseAcquisition::Fresh
        );
        assert_eq!(
            task.acquire_run_lease("scheduler-b", now, ttl),
            Err(SchedulerLeaseError::HeldByActiveOwner)
        );
        assert_eq!(
            task.renew_run_lease("scheduler-b", now + chrono::Duration::seconds(1), ttl),
            Err(SchedulerLeaseError::OwnerMismatch)
        );
        assert_eq!(
            task.release_run_lease("scheduler-b"),
            Err(SchedulerLeaseError::OwnerMismatch)
        );

        task.renew_run_lease("scheduler-a", now + chrono::Duration::seconds(20), ttl)
            .unwrap();
        let lease = task.active_run_lease.as_ref().unwrap();
        assert_eq!(lease.owner_id(), "scheduler-a");
        assert!(!lease.is_expired(now + chrono::Duration::seconds(49)));
    }

    #[test]
    fn expired_run_lease_can_be_taken_over_but_old_owner_cannot_release_it() {
        let now = Utc::now();
        let ttl = chrono::Duration::seconds(30);
        let mut task = ScheduledTask::new(300, "test".into(), true, true);

        assert_eq!(
            task.acquire_run_lease("scheduler-a", now, ttl).unwrap(),
            SchedulerLeaseAcquisition::Fresh
        );
        let takeover = now + chrono::Duration::seconds(31);
        assert_eq!(
            task.acquire_run_lease("scheduler-b", takeover, ttl)
                .unwrap(),
            SchedulerLeaseAcquisition::ReplacedExpiredLease
        );
        let receipt = task.last_run_lease_takeover.as_ref().unwrap();
        assert_eq!(receipt.previous_owner_id, "scheduler-a");
        assert_eq!(receipt.previous_heartbeat_at, now);
        assert_eq!(receipt.taken_at, takeover);
        assert_eq!(
            task.release_run_lease("scheduler-a"),
            Err(SchedulerLeaseError::OwnerMismatch)
        );
        assert_eq!(
            task.renew_run_lease("scheduler-a", takeover, ttl),
            Err(SchedulerLeaseError::OwnerMismatch)
        );
        task.release_run_lease("scheduler-b").unwrap();
        assert!(task.active_run_lease.is_none());
    }

    #[test]
    fn run_lease_rejects_invalid_owner_and_ttl() {
        let now = Utc::now();
        let mut task = ScheduledTask::new(300, "test".into(), true, true);
        assert_eq!(
            task.acquire_run_lease("", now, chrono::Duration::seconds(30)),
            Err(SchedulerLeaseError::InvalidOwner)
        );
        assert_eq!(
            task.acquire_run_lease("scheduler-a", now, chrono::Duration::zero()),
            Err(SchedulerLeaseError::InvalidTtl)
        );
    }

    #[test]
    fn failed_runs_back_off_then_dead_letter_and_success_resets_state() {
        let now = Utc::now();
        let mut task = ScheduledTask::new(1, "test".into(), true, true);
        task.last_fired_at = Some(now);

        task.record_terminal_run_status(SchedulerRunStatus::Failed, now);
        assert_eq!(task.consecutive_run_failures, 1);
        assert_eq!(
            task.retry_not_before,
            Some(now + chrono::Duration::seconds(5))
        );
        assert_eq!(task.next_due_at(), now + chrono::Duration::seconds(5));
        assert!(!task.is_dispatchable(now + chrono::Duration::seconds(4)));
        assert!(task.is_dispatchable(now + chrono::Duration::seconds(5)));

        for _ in 1..MAX_SCHEDULER_RUN_FAILURES {
            task.record_terminal_run_status(SchedulerRunStatus::Failed, now);
        }
        assert_eq!(task.consecutive_run_failures, MAX_SCHEDULER_RUN_FAILURES);
        assert!(task.dead_lettered);
        assert!(task.retry_not_before.is_none());
        assert!(!task.is_dispatchable(now + chrono::Duration::hours(1)));

        task.record_terminal_run_status(SchedulerRunStatus::Completed, now);
        assert_eq!(task.consecutive_run_failures, 0);
        assert!(!task.dead_lettered);
        assert!(task.retry_not_before.is_none());

        task.record_terminal_run_status(SchedulerRunStatus::Failed, now);
        task.clear_failure_backoff();
        assert_eq!(task.consecutive_run_failures, 0);
        assert!(!task.dead_lettered);
        assert!(task.retry_not_before.is_none());
    }

    #[test]
    fn cancelled_run_does_not_schedule_automatic_retry() {
        let now = Utc::now();
        let mut task = ScheduledTask::new(1, "test".into(), true, true);
        task.retry_not_before = Some(now + chrono::Duration::minutes(1));

        task.record_terminal_run_status(SchedulerRunStatus::Cancelled, now);

        assert!(task.retry_not_before.is_none());
        assert_eq!(task.consecutive_run_failures, 0);
    }

    #[test]
    fn active_lease_defers_overdue_dispatch_until_expiry_or_release() {
        let now = Utc::now();
        let mut task = ScheduledTask::new(1, "test".into(), true, true);
        task.last_fired_at = Some(now - chrono::Duration::seconds(10));
        task.acquire_run_lease("scheduler:foreign", now, chrono::Duration::seconds(30))
            .unwrap();

        assert_eq!(task.next_dispatch_at(), now + chrono::Duration::seconds(30));
        assert!(!task.is_dispatchable(now));
        task.release_run_lease("scheduler:foreign").unwrap();
        assert!(task.is_dispatchable(now));
    }

    #[test]
    fn current_owner_keeps_cadence_while_foreign_owner_is_deferred() {
        let now = Utc::now();
        let mut task = ScheduledTask::new(1, "test".into(), true, true);
        task.last_fired_at = Some(now - chrono::Duration::seconds(10));
        task.acquire_run_lease("scheduler:owner-a", now, chrono::Duration::seconds(30))
            .unwrap();

        assert!(task.is_dispatchable_for_owner("scheduler:owner-a", now));
        assert!(!task.is_dispatchable_for_owner("scheduler:owner-b", now));
    }

    #[test]
    fn task_id_is_12_chars() {
        let task = ScheduledTask::new(300, "test".into(), true, false);
        assert_eq!(task.id.len(), 12);
    }

    #[test]
    fn clocks_start_with_fresh_uuid_v7_generations() {
        let first = SchedulerClock::new().snapshot();
        let second = SchedulerClock::new().snapshot();

        assert_ne!(first.generation, second.generation);
        assert_eq!(first.generation.get_version_num(), 7);
        assert_eq!(first.revision(), 0);
    }

    #[test]
    fn reservation_preflights_and_commits_in_order() {
        let mut clock = SchedulerClock::new();
        let mut reservation = clock.prepare_transition(2);

        assert_eq!(clock.snapshot().revision(), 0);
        let first = reservation.commit_next(&mut clock);
        assert_eq!(first.version.revision(), 1);
        assert!(first.rollover.is_none());
        assert_eq!(reservation.commit_next(&mut clock).version.revision(), 2);
        assert_eq!(clock.snapshot().revision(), 2);

        let mut boundary = SchedulerClock::at_revision_for_test(u64::MAX - 1);
        let mut final_step = boundary.prepare_transition(1);
        assert_eq!(
            final_step.commit_next(&mut boundary).version.revision(),
            u64::MAX
        );
        let exhausted = SchedulerClock::at_revision_for_test(u64::MAX - 1);
        let old_generation = exhausted.snapshot().generation;
        let reservation = exhausted.prepare_transition(2);
        let rollover = reservation.rollover.unwrap();
        assert_eq!(rollover.old_generation, old_generation);
        assert_ne!(rollover.new_generation, old_generation);
        assert_eq!(exhausted.snapshot().revision(), u64::MAX - 1);
    }

    #[test]
    fn stale_rollover_commit_does_not_mutate_clock() {
        let mut clock = SchedulerClock::at_revision_for_test(u64::MAX);
        let mut stale = clock.prepare_transition(1);
        clock = SchedulerClock::new();
        let before = clock.snapshot();

        let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            let _ = stale.commit_next(&mut clock);
        }));

        assert!(result.is_err());
        assert_eq!(clock.snapshot(), before);
    }

    #[test]
    fn run_lease_owner_survives_scheduler_generation_rollover() {
        let mut clock = SchedulerClock::at_revision_for_test(u64::MAX);
        let owner = clock.run_lease_owner_id();
        let mut reservation = clock.prepare_transition(1);
        let commit = reservation.commit_next(&mut clock);

        assert!(commit.rollover.is_some());
        assert_eq!(clock.run_lease_owner_id(), owner);
    }
}
