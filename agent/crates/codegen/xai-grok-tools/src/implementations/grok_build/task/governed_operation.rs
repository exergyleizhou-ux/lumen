//! SessionActor-owned durable operation control plane (not a second runtime).
//!
//! Operations are root-tree scoped, append-friendly records with lease,
//! outbox, and external-effect state. Crash, late event, duplicate complete,
//! foreign takeover, and post-cancel events fail closed.

use std::collections::BTreeMap;
use std::io::ErrorKind;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::{SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};

/// High-level lifecycle for one governed operation.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum GovernedOperationState {
    Created,
    Claimed,
    Running,
    Completed,
    Failed,
    Frozen,
    Cancelled,
}

/// Outbox delivery observation for authority events.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum OutboxDeliveryState {
    Undelivered,
    Delivered,
    Uncertain,
}

/// External side-effect observation (never invent success without receipt).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ExternalEffectState {
    None,
    Pending,
    Applied,
    Unknown,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OperationDenyReason {
    NotFound,
    ForeignTree,
    ForeignOwner,
    ForeignTakeover,
    StaleLease,
    DuplicateComplete,
    LateEventAfterTerminal,
    Cancelled,
    InvalidTransition,
    IdempotencyConflict,
    BudgetAlreadyReleased,
    /// A configured durable journal could not be read or atomically persisted.
    /// Continuing in memory would allow a spawn that cannot be recovered or
    /// audited after a crash, so callers must stop instead.
    PersistenceUnavailable,
}

impl OperationDenyReason {
    pub const fn code(self) -> &'static str {
        match self {
            Self::NotFound => "op.not_found",
            Self::ForeignTree => "op.foreign_tree",
            Self::ForeignOwner => "op.foreign_owner",
            Self::ForeignTakeover => "op.foreign_takeover",
            Self::StaleLease => "op.stale_lease",
            Self::DuplicateComplete => "op.duplicate_complete",
            Self::LateEventAfterTerminal => "op.late_event_after_terminal",
            Self::Cancelled => "op.cancelled",
            Self::InvalidTransition => "op.invalid_transition",
            Self::IdempotencyConflict => "op.idempotency_conflict",
            Self::BudgetAlreadyReleased => "op.budget_already_released",
            Self::PersistenceUnavailable => "op.persistence_unavailable",
        }
    }
}

impl std::fmt::Display for OperationDenyReason {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.code())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct GovernedOperation {
    pub operation_id: String,
    pub root_tree_id: String,
    pub owner_node_id: String,
    pub kind: String,
    pub idempotency_key: String,
    pub state: GovernedOperationState,
    pub attempt: u32,
    pub lease_id: Option<String>,
    pub heartbeat_unix: u64,
    pub deadline_unix: u64,
    pub reservation_id: Option<String>,
    pub external_effect_state: ExternalEffectState,
    pub outbox_state: OutboxDeliveryState,
    pub event_sequence: u64,
    pub terminal_receipt: Option<String>,
    pub frozen_reason: Option<String>,
    pub budget_released: bool,
    pub cancelled: bool,
    pub parent_operation_id: Option<String>,
}

#[derive(Debug, Default)]
struct OperationStoreInner {
    by_id: BTreeMap<String, GovernedOperation>,
    by_idempotency: BTreeMap<(String, String), String>,
    /// Reservation ids that have already been released exactly once.
    released_reservations: BTreeMap<String, String>,
    event_seq: u64,
}

/// In-process durable store used by SessionActor. Persistence path is optional
/// for unit tests; when set, a JSON snapshot is rewritten after each mutation.
#[derive(Debug, Clone)]
pub struct GovernedOperationStore {
    root_tree_id: String,
    path: Option<PathBuf>,
    /// `false` means a configured journal was unreadable, corrupt, or failed a
    /// write. A poisoned persistent store never silently falls back to memory.
    persistence_healthy: Arc<Mutex<bool>>,
    inner: Arc<Mutex<OperationStoreInner>>,
}

fn now_unix() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

impl GovernedOperationStore {
    pub fn for_tree(root_tree_id: impl Into<String>) -> Self {
        Self {
            root_tree_id: root_tree_id.into(),
            path: None,
            persistence_healthy: Arc::new(Mutex::new(true)),
            inner: Arc::new(Mutex::new(OperationStoreInner::default())),
        }
    }

    pub fn with_path(root_tree_id: impl Into<String>, path: impl Into<PathBuf>) -> Self {
        let path = path.into();
        let mut store = Self::for_tree(root_tree_id);
        store.path = Some(path.clone());
        match std::fs::read(&path) {
            Ok(bytes) => match serde_json::from_slice::<Vec<GovernedOperation>>(&bytes) {
                Ok(ops) => {
                    let mut inner = store.inner.lock().expect("operation store lock");
                    for op in ops {
                        // A root-scoped journal must never import another tree's
                        // operation; treating it as empty would lose authority.
                        if op.root_tree_id != store.root_tree_id
                            || inner.by_id.contains_key(&op.operation_id)
                            || inner.by_idempotency.contains_key(&(
                                op.root_tree_id.clone(),
                                op.idempotency_key.clone(),
                            ))
                        {
                            store.mark_persistence_unhealthy();
                            break;
                        }
                        inner.by_idempotency.insert(
                            (op.root_tree_id.clone(), op.idempotency_key.clone()),
                            op.operation_id.clone(),
                        );
                        if op.budget_released {
                            if let Some(res) = &op.reservation_id {
                                inner
                                    .released_reservations
                                    .insert(res.clone(), op.operation_id.clone());
                            }
                        }
                        inner.event_seq = inner.event_seq.max(op.event_sequence);
                        inner.by_id.insert(op.operation_id.clone(), op);
                    }
                }
                Err(_) => store.mark_persistence_unhealthy(),
            },
            Err(error) if error.kind() == ErrorKind::NotFound => {}
            Err(_) => store.mark_persistence_unhealthy(),
        }
        store
    }

    pub fn root_tree_id(&self) -> &str {
        &self.root_tree_id
    }

    pub fn create(
        &self,
        operation_id: impl Into<String>,
        owner_node_id: impl Into<String>,
        kind: impl Into<String>,
        idempotency_key: impl Into<String>,
        reservation_id: Option<String>,
        parent_operation_id: Option<String>,
        lease_ttl_secs: u64,
    ) -> Result<GovernedOperation, OperationDenyReason> {
        self.ensure_persistence_healthy()?;
        let operation_id = operation_id.into();
        let owner_node_id = owner_node_id.into();
        let kind = kind.into();
        let idempotency_key = idempotency_key.into();
        let mut inner = self.inner.lock().expect("operation store lock");
        let key = (self.root_tree_id.clone(), idempotency_key.clone());
        if let Some(existing_id) = inner.by_idempotency.get(&key) {
            let existing = inner
                .by_id
                .get(existing_id)
                .ok_or(OperationDenyReason::NotFound)?;
            if existing.kind != kind || existing.owner_node_id != owner_node_id {
                return Err(OperationDenyReason::IdempotencyConflict);
            }
            return Ok(existing.clone());
        }
        let now = now_unix();
        inner.event_seq = inner.event_seq.saturating_add(1);
        let op = GovernedOperation {
            operation_id: operation_id.clone(),
            root_tree_id: self.root_tree_id.clone(),
            owner_node_id,
            kind,
            idempotency_key: idempotency_key.clone(),
            state: GovernedOperationState::Created,
            attempt: 0,
            lease_id: None,
            heartbeat_unix: now,
            deadline_unix: now.saturating_add(lease_ttl_secs.max(1)),
            reservation_id,
            external_effect_state: ExternalEffectState::None,
            outbox_state: OutboxDeliveryState::Undelivered,
            event_sequence: inner.event_seq,
            terminal_receipt: None,
            frozen_reason: None,
            budget_released: false,
            cancelled: false,
            parent_operation_id,
        };
        inner.by_idempotency.insert(key, operation_id.clone());
        inner.by_id.insert(operation_id, op.clone());
        self.persist(&inner)?;
        Ok(op)
    }

    pub fn claim(
        &self,
        operation_id: &str,
        owner_node_id: &str,
        lease_id: impl Into<String>,
        lease_ttl_secs: u64,
    ) -> Result<GovernedOperation, OperationDenyReason> {
        self.ensure_persistence_healthy()?;
        let mut inner = self.inner.lock().expect("operation store lock");
        let mut op = inner
            .by_id
            .get(operation_id)
            .cloned()
            .ok_or(OperationDenyReason::NotFound)?;
        self.ensure_tree(&op)?;
        if op.cancelled || matches!(op.state, GovernedOperationState::Cancelled) {
            return Err(OperationDenyReason::Cancelled);
        }
        if op.is_terminal() {
            return Err(OperationDenyReason::LateEventAfterTerminal);
        }
        if op.owner_node_id != owner_node_id {
            return Err(OperationDenyReason::ForeignOwner);
        }
        if !matches!(
            op.state,
            GovernedOperationState::Created | GovernedOperationState::Claimed
        ) {
            return Err(OperationDenyReason::InvalidTransition);
        }
        let now = now_unix();
        inner.event_seq = inner.event_seq.saturating_add(1);
        op.lease_id = Some(lease_id.into());
        op.state = GovernedOperationState::Claimed;
        op.attempt = op.attempt.saturating_add(1);
        op.heartbeat_unix = now;
        op.deadline_unix = now.saturating_add(lease_ttl_secs.max(1));
        op.outbox_state = OutboxDeliveryState::Undelivered;
        op.event_sequence = inner.event_seq;
        inner.by_id.insert(operation_id.to_owned(), op.clone());
        self.persist(&inner)?;
        Ok(op)
    }

    pub fn heartbeat(
        &self,
        operation_id: &str,
        owner_node_id: &str,
        lease_id: &str,
        lease_ttl_secs: u64,
    ) -> Result<GovernedOperation, OperationDenyReason> {
        self.ensure_persistence_healthy()?;
        let mut inner = self.inner.lock().expect("operation store lock");
        let mut op = inner
            .by_id
            .get(operation_id)
            .cloned()
            .ok_or(OperationDenyReason::NotFound)?;
        self.ensure_tree(&op)?;
        if op.cancelled {
            return Err(OperationDenyReason::Cancelled);
        }
        if op.is_terminal() {
            return Err(OperationDenyReason::LateEventAfterTerminal);
        }
        if op.owner_node_id != owner_node_id {
            return Err(OperationDenyReason::ForeignOwner);
        }
        if op.lease_id.as_deref() != Some(lease_id) {
            return Err(OperationDenyReason::StaleLease);
        }
        let now = now_unix();
        inner.event_seq = inner.event_seq.saturating_add(1);
        op.state = GovernedOperationState::Running;
        op.heartbeat_unix = now;
        op.deadline_unix = now.saturating_add(lease_ttl_secs.max(1));
        op.event_sequence = inner.event_seq;
        inner.by_id.insert(operation_id.to_owned(), op.clone());
        self.persist(&inner)?;
        Ok(op)
    }

    pub fn complete(
        &self,
        operation_id: &str,
        owner_node_id: &str,
        lease_id: &str,
        terminal_receipt: impl Into<String>,
    ) -> Result<GovernedOperation, OperationDenyReason> {
        self.ensure_persistence_healthy()?;
        self.finish(
            operation_id,
            owner_node_id,
            lease_id,
            GovernedOperationState::Completed,
            Some(terminal_receipt.into()),
            None,
        )
    }

    pub fn fail(
        &self,
        operation_id: &str,
        owner_node_id: &str,
        lease_id: &str,
        terminal_receipt: impl Into<String>,
    ) -> Result<GovernedOperation, OperationDenyReason> {
        self.finish(
            operation_id,
            owner_node_id,
            lease_id,
            GovernedOperationState::Failed,
            Some(terminal_receipt.into()),
            None,
        )
    }

    pub fn freeze(
        &self,
        operation_id: &str,
        owner_node_id: &str,
        reason: impl Into<String>,
    ) -> Result<GovernedOperation, OperationDenyReason> {
        let mut inner = self.inner.lock().expect("operation store lock");
        let mut op = inner
            .by_id
            .get(operation_id)
            .cloned()
            .ok_or(OperationDenyReason::NotFound)?;
        self.ensure_tree(&op)?;
        if op.is_terminal() && op.state != GovernedOperationState::Frozen {
            return Err(OperationDenyReason::LateEventAfterTerminal);
        }
        if op.owner_node_id != owner_node_id && owner_node_id != op.root_tree_id {
            // Only owner or root may freeze.
            return Err(OperationDenyReason::ForeignOwner);
        }
        inner.event_seq = inner.event_seq.saturating_add(1);
        op.state = GovernedOperationState::Frozen;
        op.frozen_reason = Some(reason.into());
        op.outbox_state = OutboxDeliveryState::Uncertain;
        op.external_effect_state = ExternalEffectState::Unknown;
        op.event_sequence = inner.event_seq;
        inner.by_id.insert(operation_id.to_owned(), op.clone());
        self.persist(&inner)?;
        Ok(op)
    }

    /// Take over an expired lease. Foreign non-root nodes cannot steal a live lease.
    pub fn takeover(
        &self,
        operation_id: &str,
        new_owner_node_id: &str,
        new_lease_id: impl Into<String>,
        now_unix_secs: u64,
        lease_ttl_secs: u64,
        is_root: bool,
    ) -> Result<GovernedOperation, OperationDenyReason> {
        self.ensure_persistence_healthy()?;
        let mut inner = self.inner.lock().expect("operation store lock");
        let mut op = inner
            .by_id
            .get(operation_id)
            .cloned()
            .ok_or(OperationDenyReason::NotFound)?;
        self.ensure_tree(&op)?;
        if op.cancelled {
            return Err(OperationDenyReason::Cancelled);
        }
        if op.is_terminal() {
            return Err(OperationDenyReason::LateEventAfterTerminal);
        }
        let expired = now_unix_secs > op.deadline_unix;
        if !expired && !is_root {
            return Err(OperationDenyReason::ForeignTakeover);
        }
        if !is_root && op.owner_node_id != new_owner_node_id && !expired {
            return Err(OperationDenyReason::ForeignTakeover);
        }
        inner.event_seq = inner.event_seq.saturating_add(1);
        op.owner_node_id = new_owner_node_id.to_owned();
        op.lease_id = Some(new_lease_id.into());
        op.state = GovernedOperationState::Claimed;
        op.attempt = op.attempt.saturating_add(1);
        op.heartbeat_unix = now_unix_secs;
        op.deadline_unix = now_unix_secs.saturating_add(lease_ttl_secs.max(1));
        op.event_sequence = inner.event_seq;
        inner.by_id.insert(operation_id.to_owned(), op.clone());
        self.persist(&inner)?;
        Ok(op)
    }

    /// Root cancel: mark operation + descendants cancelled, revoke leases, release
    /// each reservation at most once.
    pub fn cancel_cascade_from_root(
        &self,
        root_caller_id: &str,
        root_operation_id: &str,
    ) -> Result<Vec<GovernedOperation>, OperationDenyReason> {
        self.ensure_persistence_healthy()?;
        if root_caller_id != self.root_tree_id {
            return Err(OperationDenyReason::ForeignOwner);
        }
        let mut inner = self.inner.lock().expect("operation store lock");
        let mut targets: Vec<String> = Vec::new();
        targets.push(root_operation_id.to_owned());
        // BFS descendants by parent_operation_id
        let mut changed = true;
        while changed {
            changed = false;
            let snapshot: Vec<_> = inner.by_id.values().cloned().collect();
            for op in snapshot {
                if let Some(parent) = &op.parent_operation_id {
                    if targets.contains(parent) && !targets.contains(&op.operation_id) {
                        targets.push(op.operation_id.clone());
                        changed = true;
                    }
                }
            }
        }
        let mut out = Vec::new();
        for id in targets {
            let terminal_non_cancel = {
                let Some(op) = inner.by_id.get(&id) else {
                    continue;
                };
                if op.root_tree_id != self.root_tree_id {
                    return Err(OperationDenyReason::ForeignTree);
                }
                op.is_terminal() && op.state != GovernedOperationState::Cancelled
            };
            if terminal_non_cancel {
                // Already finished: do not resurrect; still ensure budget release once.
                Self::release_budget_once(&mut inner, &id)?;
                if let Some(op) = inner.by_id.get(&id) {
                    out.push(op.clone());
                }
                continue;
            }
            inner.event_seq = inner.event_seq.saturating_add(1);
            let seq = inner.event_seq;
            if let Some(op) = inner.by_id.get_mut(&id) {
                op.cancelled = true;
                op.state = GovernedOperationState::Cancelled;
                op.lease_id = None;
                op.outbox_state = OutboxDeliveryState::Undelivered;
                op.event_sequence = seq;
            }
            Self::release_budget_once(&mut inner, &id)?;
            if let Some(op) = inner.by_id.get(&id) {
                out.push(op.clone());
            }
        }
        self.persist(&inner)?;
        Ok(out)
    }

    pub fn mark_outbox_delivered(
        &self,
        operation_id: &str,
    ) -> Result<GovernedOperation, OperationDenyReason> {
        self.ensure_persistence_healthy()?;
        let mut inner = self.inner.lock().expect("operation store lock");
        let mut op = inner
            .by_id
            .get(operation_id)
            .cloned()
            .ok_or(OperationDenyReason::NotFound)?;
        self.ensure_tree(&op)?;
        inner.event_seq = inner.event_seq.saturating_add(1);
        op.outbox_state = OutboxDeliveryState::Delivered;
        op.event_sequence = inner.event_seq;
        inner.by_id.insert(operation_id.to_owned(), op.clone());
        self.persist(&inner)?;
        Ok(op)
    }

    /// A terminal result was produced, but every authoritative receiver that
    /// was expected to observe it had already gone away.  Keep the terminal
    /// operation durable, while making its delivery truth explicitly
    /// uncertain; callers must not read this as a successful handoff.
    pub fn mark_outbox_uncertain(
        &self,
        operation_id: &str,
    ) -> Result<GovernedOperation, OperationDenyReason> {
        self.ensure_persistence_healthy()?;
        let mut inner = self.inner.lock().expect("operation store lock");
        let mut op = inner
            .by_id
            .get(operation_id)
            .cloned()
            .ok_or(OperationDenyReason::NotFound)?;
        self.ensure_tree(&op)?;
        inner.event_seq = inner.event_seq.saturating_add(1);
        op.outbox_state = OutboxDeliveryState::Uncertain;
        op.event_sequence = inner.event_seq;
        inner.by_id.insert(operation_id.to_owned(), op.clone());
        self.persist(&inner)?;
        Ok(op)
    }

    pub fn get(&self, operation_id: &str) -> Result<GovernedOperation, OperationDenyReason> {
        let inner = self.inner.lock().expect("operation store lock");
        let op = inner
            .by_id
            .get(operation_id)
            .ok_or(OperationDenyReason::NotFound)?;
        self.ensure_tree(op)?;
        Ok(op.clone())
    }

    pub fn list(&self) -> Vec<GovernedOperation> {
        let inner = self.inner.lock().expect("operation store lock");
        inner.by_id.values().cloned().collect()
    }

    fn finish(
        &self,
        operation_id: &str,
        owner_node_id: &str,
        lease_id: &str,
        state: GovernedOperationState,
        terminal_receipt: Option<String>,
        frozen_reason: Option<String>,
    ) -> Result<GovernedOperation, OperationDenyReason> {
        self.ensure_persistence_healthy()?;
        let mut inner = self.inner.lock().expect("operation store lock");
        let mut op = inner
            .by_id
            .get(operation_id)
            .cloned()
            .ok_or(OperationDenyReason::NotFound)?;
        self.ensure_tree(&op)?;
        if op.cancelled {
            return Err(OperationDenyReason::Cancelled);
        }
        if op.is_terminal() {
            // Completed/failed twice is a duplicate; frozen/cancelled late events
            // must not resurrect the operation either.
            return Err(
                if matches!(
                    op.state,
                    GovernedOperationState::Completed | GovernedOperationState::Failed
                ) {
                    OperationDenyReason::DuplicateComplete
                } else {
                    OperationDenyReason::LateEventAfterTerminal
                },
            );
        }
        if op.owner_node_id != owner_node_id {
            return Err(OperationDenyReason::ForeignOwner);
        }
        if op.lease_id.as_deref() != Some(lease_id) {
            return Err(OperationDenyReason::StaleLease);
        }
        inner.event_seq = inner.event_seq.saturating_add(1);
        op.state = state;
        op.terminal_receipt = terminal_receipt;
        op.frozen_reason = frozen_reason;
        op.lease_id = None;
        op.outbox_state = OutboxDeliveryState::Undelivered;
        if matches!(state, GovernedOperationState::Completed) {
            op.external_effect_state = ExternalEffectState::Applied;
        }
        op.event_sequence = inner.event_seq;
        let id = op.operation_id.clone();
        inner.by_id.insert(id.clone(), op);
        Self::release_budget_once(&mut inner, &id)?;
        let out = inner
            .by_id
            .get(&id)
            .cloned()
            .ok_or(OperationDenyReason::NotFound)?;
        self.persist(&inner)?;
        Ok(out)
    }

    fn release_budget_once(
        inner: &mut OperationStoreInner,
        operation_id: &str,
    ) -> Result<(), OperationDenyReason> {
        let Some(op) = inner.by_id.get_mut(operation_id) else {
            return Err(OperationDenyReason::NotFound);
        };
        let Some(res) = op.reservation_id.clone() else {
            op.budget_released = true;
            return Ok(());
        };
        if op.budget_released {
            return Ok(());
        }
        if let Some(prior) = inner.released_reservations.get(&res) {
            if prior != operation_id {
                return Err(OperationDenyReason::BudgetAlreadyReleased);
            }
            op.budget_released = true;
            return Ok(());
        }
        inner
            .released_reservations
            .insert(res, operation_id.to_owned());
        op.budget_released = true;
        Ok(())
    }

    fn ensure_tree(&self, op: &GovernedOperation) -> Result<(), OperationDenyReason> {
        if op.root_tree_id != self.root_tree_id {
            return Err(OperationDenyReason::ForeignTree);
        }
        Ok(())
    }

    fn ensure_persistence_healthy(&self) -> Result<(), OperationDenyReason> {
        if *self
            .persistence_healthy
            .lock()
            .expect("operation persistence state lock")
        {
            Ok(())
        } else {
            Err(OperationDenyReason::PersistenceUnavailable)
        }
    }

    fn mark_persistence_unhealthy(&self) {
        *self
            .persistence_healthy
            .lock()
            .expect("operation persistence state lock") = false;
    }

    fn persist(&self, inner: &OperationStoreInner) -> Result<(), OperationDenyReason> {
        let Some(path) = &self.path else {
            return Ok(());
        };
        let result = (|| {
            let parent = path.parent().ok_or_else(|| {
                std::io::Error::new(ErrorKind::InvalidInput, "journal path has no parent")
            })?;
            std::fs::create_dir_all(parent)?;
            let ops: Vec<_> = inner.by_id.values().cloned().collect();
            let bytes = serde_json::to_vec_pretty(&ops).map_err(std::io::Error::other)?;
            let mut temporary = tempfile::NamedTempFile::new_in(parent)?;
            use std::io::Write;
            temporary.write_all(&bytes)?;
            temporary.as_file().sync_all()?;
            temporary.persist(path).map_err(|error| error.error)?;
            Ok::<(), std::io::Error>(())
        })();
        if result.is_err() {
            self.mark_persistence_unhealthy();
            return Err(OperationDenyReason::PersistenceUnavailable);
        }
        Ok(())
    }
}

impl GovernedOperation {
    pub fn is_terminal(&self) -> bool {
        matches!(
            self.state,
            GovernedOperationState::Completed
                | GovernedOperationState::Failed
                | GovernedOperationState::Frozen
                | GovernedOperationState::Cancelled
        )
    }
}

/// Optional budget reservation ledger used by cancel-cascade proofs.
#[derive(Debug, Default, Clone)]
pub struct TreeBudgetLedger {
    /// reservation_id -> still held
    held: BTreeMap<String, bool>,
}

impl TreeBudgetLedger {
    pub fn reserve(&mut self, reservation_id: impl Into<String>) {
        self.held.insert(reservation_id.into(), true);
    }

    pub fn release_once(&mut self, reservation_id: &str) -> Result<(), OperationDenyReason> {
        match self.held.get_mut(reservation_id) {
            Some(held) if *held => {
                *held = false;
                Ok(())
            }
            Some(_) => Err(OperationDenyReason::BudgetAlreadyReleased),
            None => Err(OperationDenyReason::NotFound),
        }
    }

    pub fn is_held(&self, reservation_id: &str) -> bool {
        self.held.get(reservation_id).copied().unwrap_or(false)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn create_claim_heartbeat_complete_and_duplicate_fail_closed() {
        let store = GovernedOperationStore::for_tree("root");
        let op = store
            .create(
                "op-1",
                "root",
                "child_spawn",
                "idem-1",
                Some("res-1".into()),
                None,
                30,
            )
            .unwrap();
        assert_eq!(op.state, GovernedOperationState::Created);
        let claimed = store.claim("op-1", "root", "lease-a", 30).unwrap();
        assert_eq!(claimed.state, GovernedOperationState::Claimed);
        store.heartbeat("op-1", "root", "lease-a", 30).unwrap();
        let done = store
            .complete("op-1", "root", "lease-a", "receipt://done")
            .unwrap();
        assert_eq!(done.state, GovernedOperationState::Completed);
        assert!(done.budget_released);
        assert_eq!(
            store
                .complete("op-1", "root", "lease-a", "receipt://again")
                .unwrap_err(),
            OperationDenyReason::DuplicateComplete
        );
    }

    #[test]
    fn foreign_takeover_and_late_event_after_cancel_fail_closed() {
        let store = GovernedOperationStore::for_tree("root");
        store
            .create(
                "op-1",
                "child",
                "work",
                "idem-1",
                Some("res-1".into()),
                None,
                30,
            )
            .unwrap();
        store.claim("op-1", "child", "lease-a", 30).unwrap();
        assert_eq!(
            store
                .takeover("op-1", "other", "lease-b", now_unix(), 30, false)
                .unwrap_err(),
            OperationDenyReason::ForeignTakeover
        );
        // Expired lease may be taken by non-root.
        let past = now_unix().saturating_sub(1000);
        {
            let mut inner = store.inner.lock().unwrap();
            if let Some(op) = inner.by_id.get_mut("op-1") {
                op.deadline_unix = past;
            }
        }
        store
            .takeover("op-1", "other", "lease-b", now_unix(), 30, false)
            .unwrap();
        store.cancel_cascade_from_root("root", "op-1").unwrap();
        assert_eq!(
            store.heartbeat("op-1", "other", "lease-b", 30).unwrap_err(),
            OperationDenyReason::Cancelled
        );
    }

    #[test]
    fn cancel_cascade_releases_budget_exactly_once_for_descendants() {
        let store = GovernedOperationStore::for_tree("root");
        store
            .create(
                "op-root",
                "root",
                "tree",
                "idem-root",
                Some("res-root".into()),
                None,
                30,
            )
            .unwrap();
        store
            .create(
                "op-child",
                "child",
                "work",
                "idem-child",
                Some("res-child".into()),
                Some("op-root".into()),
                30,
            )
            .unwrap();
        store
            .create(
                "op-leaf",
                "leaf",
                "evidence",
                "idem-leaf",
                Some("res-leaf".into()),
                Some("op-child".into()),
                30,
            )
            .unwrap();
        let cancelled = store.cancel_cascade_from_root("root", "op-root").unwrap();
        assert_eq!(cancelled.len(), 3);
        for op in store.list() {
            assert_eq!(op.state, GovernedOperationState::Cancelled);
            assert!(op.budget_released);
            assert!(op.lease_id.is_none());
        }
        // Second cancel must not double-release.
        let again = store.cancel_cascade_from_root("root", "op-root").unwrap();
        assert!(again.iter().all(|op| op.budget_released));
    }

    #[test]
    fn freeze_marks_uncertain_outbox_without_resurrecting() {
        let store = GovernedOperationStore::for_tree("root");
        store
            .create("op-1", "root", "ext", "idem-1", None, None, 30)
            .unwrap();
        let frozen = store
            .freeze("op-1", "root", "missing external receipt")
            .unwrap();
        assert_eq!(frozen.state, GovernedOperationState::Frozen);
        assert_eq!(frozen.outbox_state, OutboxDeliveryState::Uncertain);
        assert_eq!(
            store.complete("op-1", "root", "no-lease", "r").unwrap_err(),
            OperationDenyReason::LateEventAfterTerminal
        );
    }

    #[test]
    fn outbox_observations_are_durable_and_strictly_advance_event_sequence() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("ops.json");
        let store = GovernedOperationStore::with_path("root", &path);
        store
            .create("op-1", "root", "work", "idem-1", None, None, 30)
            .unwrap();
        store.claim("op-1", "root", "lease-1", 30).unwrap();
        let completed = store
            .complete("op-1", "root", "lease-1", "receipt://done")
            .unwrap();
        let delivered = store.mark_outbox_delivered("op-1").unwrap();
        assert_eq!(delivered.outbox_state, OutboxDeliveryState::Delivered);
        assert!(delivered.event_sequence > completed.event_sequence);
        let uncertain = store.mark_outbox_uncertain("op-1").unwrap();
        assert_eq!(uncertain.outbox_state, OutboxDeliveryState::Uncertain);
        assert!(uncertain.event_sequence > delivered.event_sequence);

        let reopened = GovernedOperationStore::with_path("root", &path);
        assert_eq!(reopened.get("op-1").unwrap(), uncertain);
    }

    #[test]
    fn tree_budget_release_is_exactly_once() {
        let mut budget = TreeBudgetLedger::default();
        budget.reserve("r1");
        assert!(budget.is_held("r1"));
        budget.release_once("r1").unwrap();
        assert!(!budget.is_held("r1"));
        assert_eq!(
            budget.release_once("r1").unwrap_err(),
            OperationDenyReason::BudgetAlreadyReleased
        );
    }

    #[test]
    fn shared_reservation_cannot_release_budget_twice_across_ops() {
        // Two ops referencing the SAME reservation must fail closed: whichever
        // completes first releases the budget; the second is refused instead
        // of silently double-releasing.
        let store = GovernedOperationStore::for_tree("root");
        store
            .create(
                "op-a",
                "root",
                "work",
                "idem-a",
                Some("shared-res".into()),
                None,
                30,
            )
            .unwrap();
        store
            .create(
                "op-b",
                "root",
                "work",
                "idem-b",
                Some("shared-res".into()),
                None,
                30,
            )
            .unwrap();
        store.claim("op-a", "root", "lease-a", 30).unwrap();
        store.claim("op-b", "root", "lease-b", 30).unwrap();

        let done = store
            .complete("op-a", "root", "lease-a", "receipt://a")
            .unwrap();
        assert!(done.budget_released);

        let err = store
            .complete("op-b", "root", "lease-b", "receipt://b")
            .unwrap_err();
        assert_eq!(err, OperationDenyReason::BudgetAlreadyReleased);
        assert!(
            !store.get("op-b").unwrap().budget_released,
            "op-b must not claim the shared budget release"
        );
    }

    #[test]
    fn persisted_store_recovers_terminal_receipt_and_budget_release() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("operations.json");
        let first = GovernedOperationStore::with_path("root", &path);
        first
            .create(
                "op-1",
                "root",
                "child_spawn",
                "idem-1",
                Some("reservation-1".into()),
                None,
                30,
            )
            .unwrap();
        first.claim("op-1", "root", "lease-1", 30).unwrap();
        first
            .complete("op-1", "root", "lease-1", "receipt://done")
            .unwrap();
        drop(first);

        let recovered = GovernedOperationStore::with_path("root", &path);
        let op = recovered.get("op-1").unwrap();
        assert_eq!(op.state, GovernedOperationState::Completed);
        assert_eq!(op.terminal_receipt.as_deref(), Some("receipt://done"));
        assert!(op.budget_released);
        assert_eq!(
            recovered
                .create(
                    "op-duplicate",
                    "root",
                    "child_spawn",
                    "idem-1",
                    Some("reservation-1".into()),
                    None,
                    30,
                )
                .unwrap()
                .operation_id,
            "op-1",
            "restart must preserve idempotency identity"
        );
        assert_eq!(
            recovered
                .complete("op-1", "root", "lease-1", "receipt://again")
                .unwrap_err(),
            OperationDenyReason::DuplicateComplete,
            "restart must not permit a duplicate external completion"
        );
    }

    #[test]
    fn corrupt_persisted_journal_fails_closed_instead_of_starting_empty() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("operations.json");
        std::fs::write(&path, b"{not valid json").unwrap();

        let store = GovernedOperationStore::with_path("root", &path);
        assert_eq!(
            store
                .create("op-1", "root", "work", "idem-1", None, None, 30)
                .unwrap_err(),
            OperationDenyReason::PersistenceUnavailable
        );
        assert!(store.list().is_empty());
    }

    #[test]
    fn foreign_root_persisted_journal_fails_closed() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("operations.json");
        let foreign = GovernedOperationStore::with_path("foreign-root", &path);
        foreign
            .create(
                "foreign-op",
                "foreign-root",
                "work",
                "idem-1",
                None,
                None,
                30,
            )
            .unwrap();
        drop(foreign);

        let root = GovernedOperationStore::with_path("root", &path);
        assert_eq!(
            root.create("op-1", "root", "work", "idem-1", None, None, 30)
                .unwrap_err(),
            OperationDenyReason::PersistenceUnavailable,
            "a root must not load another tree's durable operations"
        );
    }

    #[test]
    fn failed_atomic_persist_returns_error_and_poisoned_store_rejects_retry() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("operations.json");
        let store = GovernedOperationStore::with_path("root", &path);
        // The store was opened while the target was absent (a healthy empty
        // journal). Replacing that target with a directory makes the atomic
        // rename fail without relying on platform-specific permissions.
        std::fs::create_dir(&path).unwrap();

        assert_eq!(
            store
                .create("op-1", "root", "work", "idem-1", None, None, 30)
                .unwrap_err(),
            OperationDenyReason::PersistenceUnavailable
        );
        assert_eq!(
            store
                .create("op-2", "root", "work", "idem-2", None, None, 30)
                .unwrap_err(),
            OperationDenyReason::PersistenceUnavailable,
            "a failed journal must not silently fall back to memory on retry"
        );
    }
}
