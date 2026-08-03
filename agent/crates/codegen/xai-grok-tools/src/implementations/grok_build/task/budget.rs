//! NG-03B — TreeBudgetV1：per-tree 原子预算账本。
//!
//! Coordinator 现有实现（`tree_total_tokens_used` 等）是"检查后递增"；
//! 本模块把 reserve/release/settle 固化为原子 check-and-reserve 账本：
//! - `reserve_spawn` 是原子 check+reserve（上限检查与占用不可拆分）；
//! - `release` 幂等（重复 release 是 no-op）；
//! - `settle_usage` exactly-once（重复 settle 拒绝）；usage 不可得时标记
//!   unknown 且**不扣减、不记零**；
//! - late completion（已 release 后的 settle）fail-closed（NotFound）。
//!
//! 纯内存账本；持久化/replay 属 NG-03C operation journal 范围。

use std::collections::HashMap;
use std::time::Duration;

/// 树级预算配置（执行书 NG-03B 【拟建】 TreeBudgetV1）。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TreeBudgetV1 {
    pub max_depth: u8,
    pub max_children_per_node: u8,
    pub max_live_nodes: u16,
    pub max_background_nodes: u16,
    pub token_reservation_limit: Option<u64>,
    pub tool_call_limit: Option<u32>,
    pub wall_time_limit: Duration,
    /// 日成本上限（以最小费用单位计）；仅配置，结算在操作层。
    pub daily_cost_limit: Option<u64>,
    /// artifact 字节上限；仅配置，结算在 artifact 层。
    pub artifact_byte_limit: Option<u64>,
}

impl Default for TreeBudgetV1 {
    fn default() -> Self {
        Self {
            max_depth: 3,
            max_children_per_node: 8,
            max_live_nodes: 32,
            max_background_nodes: 8,
            token_reservation_limit: None,
            tool_call_limit: None,
            wall_time_limit: Duration::from_secs(6 * 60 * 60),
            daily_cost_limit: None,
            artifact_byte_limit: None,
        }
    }
}

/// 一次 spawn 的预留凭证。
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct ReservationId(pub u64);

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BudgetDenial {
    DepthExceeded { max_depth: u8 },
    ChildrenPerNodeExceeded { node: String, max: u8 },
    LiveNodesExceeded { max: u16 },
    BackgroundNodesExceeded { max: u16 },
    TokenReservationExceeded { limit: u64 },
    ToolReservationExceeded { limit: u32 },
    TreeExpired,
}

impl std::fmt::Display for BudgetDenial {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            BudgetDenial::DepthExceeded { max_depth } => {
                write!(f, "depth exceeds max_depth {max_depth}")
            }
            BudgetDenial::ChildrenPerNodeExceeded { node, max } => {
                write!(f, "node {node} exceeds max_children_per_node {max}")
            }
            BudgetDenial::LiveNodesExceeded { max } => {
                write!(f, "live nodes exceed max_live_nodes {max}")
            }
            BudgetDenial::BackgroundNodesExceeded { max } => {
                write!(f, "background nodes exceed max_background_nodes {max}")
            }
            BudgetDenial::TokenReservationExceeded { limit } => {
                write!(f, "token reservation exceeds limit {limit}")
            }
            BudgetDenial::ToolReservationExceeded { limit } => {
                write!(f, "tool reservation exceeds limit {limit}")
            }
            BudgetDenial::TreeExpired => write!(f, "tree budget expired"),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ReleaseOutcome {
    Released,
    AlreadyReleased,
    NotFound,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UsageSettlement {
    Applied,
    AlreadySettled,
    UnknownUsageNotDebited,
    NotFound,
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum ReservationState {
    Reserved,
    Settled,
    Released,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct Reservation {
    id: ReservationId,
    node_id: String,
    depth: u8,
    background: bool,
    tokens_reserved: u64,
    tools_reserved: u32,
    state: ReservationState,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LiveNode {
    pub node_id: String,
    pub parent: Option<String>,
    pub depth: u8,
    pub background: bool,
    pub reservations: Vec<ReservationId>,
}

/// 单一 root tree 的原子预算账本。`Arc<Mutex<BudgetLedger>>` 下并发安全。
#[derive(Debug, Clone)]
pub struct BudgetLedger {
    budget: TreeBudgetV1,
    live: HashMap<String, LiveNode>,
    reservations: HashMap<u64, Reservation>,
    next_reservation_id: u64,
    tree_tokens_settled: u64,
    tree_tools_settled: u64,
    tree_expired: bool,
    usage_unknown: Vec<ReservationId>,
}

impl BudgetLedger {
    pub fn new(budget: TreeBudgetV1) -> Self {
        Self {
            budget,
            live: HashMap::new(),
            reservations: HashMap::new(),
            next_reservation_id: 1,
            tree_tokens_settled: 0,
            tree_tools_settled: 0,
            tree_expired: false,
            usage_unknown: Vec::new(),
        }
    }

    pub fn budget(&self) -> &TreeBudgetV1 {
        &self.budget
    }

    pub fn expire_tree(&mut self) {
        self.tree_expired = true;
    }

    pub fn is_expired(&self) -> bool {
        self.tree_expired
    }

    /// 原子 check + reserve。任何上限被突破都整体拒绝，不产生部分占用。
    pub fn reserve_spawn(
        &mut self,
        node_id: impl Into<String>,
        parent: Option<&str>,
        depth: u8,
        background: bool,
        tokens_reserved: u64,
        tools_reserved: u32,
    ) -> Result<ReservationId, BudgetDenial> {
        if self.tree_expired {
            return Err(BudgetDenial::TreeExpired);
        }
        if depth > self.budget.max_depth {
            return Err(BudgetDenial::DepthExceeded {
                max_depth: self.budget.max_depth,
            });
        }
        if let Some(parent) = parent {
            let siblings = self
                .live
                .values()
                .filter(|node| node.parent.as_deref() == Some(parent))
                .count();
            if siblings >= usize::from(self.budget.max_children_per_node) {
                return Err(BudgetDenial::ChildrenPerNodeExceeded {
                    node: parent.to_owned(),
                    max: self.budget.max_children_per_node,
                });
            }
        }
        let live_total = self.live.len() as u16;
        if live_total >= self.budget.max_live_nodes {
            return Err(BudgetDenial::LiveNodesExceeded {
                max: self.budget.max_live_nodes,
            });
        }
        if background {
            let background_total = self
                .live
                .values()
                .filter(|node| node.background)
                .count() as u16;
            if background_total >= self.budget.max_background_nodes {
                return Err(BudgetDenial::BackgroundNodesExceeded {
                    max: self.budget.max_background_nodes,
                });
            }
        }
        let reserved_tokens: u64 = self
            .reservations
            .values()
            .filter(|r| r.state == ReservationState::Reserved || r.state == ReservationState::Settled)
            .map(|r| r.tokens_reserved)
            .sum();
        if let Some(limit) = self.budget.token_reservation_limit
            && self.tree_tokens_settled + reserved_tokens + tokens_reserved > limit
        {
            return Err(BudgetDenial::TokenReservationExceeded { limit });
        }
        let reserved_tools: u64 = self
            .reservations
            .values()
            .filter(|r| r.state == ReservationState::Reserved || r.state == ReservationState::Settled)
            .map(|r| u64::from(r.tools_reserved))
            .sum();
        if let Some(limit) = self.budget.tool_call_limit
            && self.tree_tools_settled + reserved_tools + u64::from(tools_reserved) > u64::from(limit)
        {
            return Err(BudgetDenial::ToolReservationExceeded { limit });
        }

        let id = ReservationId(self.next_reservation_id);
        self.next_reservation_id += 1;
        let node_id = node_id.into();
        self.reservations.insert(
            id.0,
            Reservation {
                id,
                node_id: node_id.clone(),
                depth,
                background,
                tokens_reserved,
                tools_reserved,
                state: ReservationState::Reserved,
            },
        );
        self.live
            .entry(node_id.clone())
            .and_modify(|node| node.reservations.push(id))
            .or_insert(LiveNode {
                node_id,
                parent: parent.map(str::to_owned),
                depth,
                background,
                reservations: vec![id],
            });
        Ok(id)
    }

    /// 幂等释放。节点所有 reservation 都释放后才从 live 表移除。
    pub fn release(&mut self, id: ReservationId) -> ReleaseOutcome {
        let Some(reservation) = self.reservations.get_mut(&id.0) else {
            return ReleaseOutcome::NotFound;
        };
        let node_id = reservation.node_id.clone();
        match reservation.state {
            ReservationState::Released => return ReleaseOutcome::AlreadyReleased,
            ReservationState::Settled => {
                // Settled reservations already released their accounting when
                // settled; marking released is idempotent.
                reservation.state = ReservationState::Released;
                self.detach_if_idle(&node_id);
                return ReleaseOutcome::AlreadyReleased;
            }
            ReservationState::Reserved => {}
        }
        reservation.state = ReservationState::Released;
        self.detach_if_idle(&node_id);
        ReleaseOutcome::Released
    }

    fn detach_if_idle(&mut self, node_id: &str) {
        let Some(node) = self.live.get(node_id) else {
            return;
        };
        let all_released = node.reservations.iter().all(|id| {
            matches!(
                self.reservations.get(&id.0).map(|r| &r.state),
                Some(ReservationState::Released)
            )
        });
        if all_released {
            self.live.remove(node_id);
        }
    }

    /// exactly-once 结算。usage 不可得（`None`）时不扣减、不记零，标记
    /// unknown；已 release 的 late settle fail-closed。
    pub fn settle_usage(
        &mut self,
        id: ReservationId,
        tokens: Option<u64>,
        tools: Option<u32>,
    ) -> UsageSettlement {
        let Some(reservation) = self.reservations.get_mut(&id.0) else {
            return UsageSettlement::NotFound;
        };
        match reservation.state {
            ReservationState::Settled => return UsageSettlement::AlreadySettled,
            ReservationState::Released => return UsageSettlement::NotFound,
            ReservationState::Reserved => {}
        }
        reservation.state = ReservationState::Settled;
        match (tokens, tools) {
            (None, None) => {
                self.usage_unknown.push(id);
                UsageSettlement::UnknownUsageNotDebited
            }
            (tokens, tools) => {
                self.tree_tokens_settled += tokens.unwrap_or(0);
                self.tree_tools_settled += u64::from(tools.unwrap_or(0));
                UsageSettlement::Applied
            }
        }
    }

    pub fn live_node_count(&self) -> usize {
        self.live.len()
    }

    pub fn settled_tokens(&self) -> u64 {
        self.tree_tokens_settled
    }

    pub fn settled_tools(&self) -> u64 {
        self.tree_tools_settled
    }

    pub fn usage_unknown(&self) -> &[ReservationId] {
        &self.usage_unknown
    }

    pub fn live_nodes(&self) -> impl Iterator<Item = &LiveNode> {
        self.live.values()
    }
}

#[cfg(test)]
mod budget_tests {
    use super::*;

    fn ledger() -> BudgetLedger {
        BudgetLedger::new(TreeBudgetV1 {
            max_depth: 3,
            max_children_per_node: 64,
            max_live_nodes: 4,
            max_background_nodes: 2,
            token_reservation_limit: Some(1000),
            tool_call_limit: Some(10),
            wall_time_limit: Duration::from_secs(3600),
            daily_cost_limit: None,
            artifact_byte_limit: None,
        })
    }

    #[test]
    fn reserve_release_round_trip() {
        let mut l = ledger();
        let id = l.reserve_spawn("child-1", Some("root"), 1, false, 100, 1).unwrap();
        assert_eq!(l.live_node_count(), 1);
        assert_eq!(l.release(id), ReleaseOutcome::Released);
        assert_eq!(l.live_node_count(), 0, "idle node must detach");
    }

    #[test]
    fn release_is_idempotent() {
        let mut l = ledger();
        let id = l.reserve_spawn("child-1", Some("root"), 1, false, 0, 0).unwrap();
        assert_eq!(l.release(id), ReleaseOutcome::Released);
        assert_eq!(l.release(id), ReleaseOutcome::AlreadyReleased);
        assert_eq!(l.release(ReservationId(999)), ReleaseOutcome::NotFound);
    }

    #[test]
    fn live_node_limit_denies_overflow() {
        let mut l = ledger();
        for i in 0..4 {
            l.reserve_spawn(format!("c-{i}"), Some("root"), 1, false, 0, 0).unwrap();
        }
        assert_eq!(
            l.reserve_spawn("c-over", Some("root"), 1, false, 0, 0),
            Err(BudgetDenial::LiveNodesExceeded { max: 4 })
        );
    }

    #[test]
    fn children_per_node_limit_denies_overflow() {
        let mut l = BudgetLedger::new(TreeBudgetV1 {
            max_depth: 3,
            max_children_per_node: 2,
            max_live_nodes: 64,
            max_background_nodes: 64,
            token_reservation_limit: None,
            tool_call_limit: None,
            wall_time_limit: Duration::from_secs(3600),
            daily_cost_limit: None,
            artifact_byte_limit: None,
        });
        l.reserve_spawn("c-1", Some("root"), 1, false, 0, 0).unwrap();
        l.reserve_spawn("c-2", Some("root"), 1, false, 0, 0).unwrap();
        assert_eq!(
            l.reserve_spawn("c-3", Some("root"), 1, false, 0, 0),
            Err(BudgetDenial::ChildrenPerNodeExceeded { node: "root".into(), max: 2 })
        );
        // A different parent is unaffected.
        l.reserve_spawn("other-1", Some("other"), 1, false, 0, 0).unwrap();
    }

    #[test]
    fn background_limit_denies_overflow() {
        let mut l = ledger();
        l.reserve_spawn("bg-1", Some("root"), 1, true, 0, 0).unwrap();
        l.reserve_spawn("bg-2", Some("root"), 1, true, 0, 0).unwrap();
        assert_eq!(
            l.reserve_spawn("bg-3", Some("root"), 1, true, 0, 0),
            Err(BudgetDenial::BackgroundNodesExceeded { max: 2 })
        );
    }

    #[test]
    fn depth_and_expiry_deny() {
        let mut l = ledger();
        assert_eq!(
            l.reserve_spawn("deep", Some("root"), 4, false, 0, 0),
            Err(BudgetDenial::DepthExceeded { max_depth: 3 })
        );
        l.expire_tree();
        assert_eq!(
            l.reserve_spawn("late", Some("root"), 1, false, 0, 0),
            Err(BudgetDenial::TreeExpired)
        );
    }

    #[test]
    fn token_and_tool_ceilings_are_reserve_aware() {
        let mut l = ledger();
        // 900 reserved + 100 settled (via settle) must block a 100-token request.
        let a = l.reserve_spawn("a", Some("root"), 1, false, 900, 5).unwrap();
        assert_eq!(l.settle_usage(a, Some(100), Some(5)), UsageSettlement::Applied);
        assert_eq!(
            l.reserve_spawn("b", Some("root"), 1, false, 100, 0),
            Err(BudgetDenial::TokenReservationExceeded { limit: 1000 })
        );
        assert_eq!(
            l.reserve_spawn("c", Some("root"), 1, false, 0, 6),
            Err(BudgetDenial::ToolReservationExceeded { limit: 10 })
        );
    }

    #[test]
    fn settle_is_exactly_once() {
        let mut l = ledger();
        let id = l.reserve_spawn("a", Some("root"), 1, false, 100, 2).unwrap();
        assert_eq!(l.settle_usage(id, Some(50), Some(1)), UsageSettlement::Applied);
        assert_eq!(l.settled_tokens(), 50);
        assert_eq!(l.settle_usage(id, Some(50), Some(1)), UsageSettlement::AlreadySettled);
        assert_eq!(l.settled_tokens(), 50, "second settle must not double-count");
    }

    #[test]
    fn usage_unavailable_is_not_debited_and_not_zeroed() {
        let mut l = ledger();
        let id = l.reserve_spawn("a", Some("root"), 1, false, 100, 2).unwrap();
        assert_eq!(
            l.settle_usage(id, None, None),
            UsageSettlement::UnknownUsageNotDebited
        );
        assert_eq!(l.settled_tokens(), 0);
        assert_eq!(l.settled_tools(), 0);
        assert_eq!(l.usage_unknown(), &[id]);
    }

    #[test]
    fn late_settle_after_release_fails_closed() {
        let mut l = ledger();
        let id = l.reserve_spawn("a", Some("root"), 1, false, 100, 2).unwrap();
        assert_eq!(l.release(id), ReleaseOutcome::Released);
        assert_eq!(
            l.settle_usage(id, Some(50), Some(1)),
            UsageSettlement::NotFound,
            "late completion must not revive or debit a released reservation"
        );
        assert_eq!(l.settled_tokens(), 0);
    }

    #[test]
    fn node_detaches_only_when_all_its_reservations_release() {
        let mut l = ledger();
        let a = l.reserve_spawn("a", Some("root"), 1, false, 10, 1).unwrap();
        let b = l.reserve_spawn("a", Some("root"), 1, false, 10, 1).unwrap();
        assert_eq!(l.live_node_count(), 1, "same node keeps one live entry");
        l.release(a);
        assert_eq!(l.live_node_count(), 1, "second reservation keeps node live");
        l.release(b);
        assert_eq!(l.live_node_count(), 0);
    }

    #[test]
    fn concurrent_reserves_never_exceed_the_ceiling() {
        use std::sync::{Arc, Mutex};
        use std::thread;

        let ledger = Arc::new(Mutex::new(BudgetLedger::new(TreeBudgetV1 {
            max_depth: 3,
            max_children_per_node: 64,
            max_live_nodes: 16,
            max_background_nodes: 16,
            token_reservation_limit: None,
            tool_call_limit: None,
            wall_time_limit: Duration::from_secs(3600),
            daily_cost_limit: None,
            artifact_byte_limit: None,
        })));
        let mut handles = Vec::new();
        for t in 0..8 {
            let ledger = ledger.clone();
            handles.push(thread::spawn(move || {
                let mut accepted = 0;
                for i in 0..8 {
                    let node = format!("t{t}-c{i}");
                    let result = ledger
                        .lock()
                        .unwrap()
                        .reserve_spawn(node, Some("root"), 1, false, 0, 0);
                    if result.is_ok() {
                        accepted += 1;
                    }
                }
                accepted
            }));
        }
        let total: usize = handles
            .into_iter()
            .map(|h| h.join().unwrap())
            .sum();
        assert_eq!(total, 16, "accepted total must equal the ceiling exactly");
        assert_eq!(ledger.lock().unwrap().live_node_count(), 16);
    }
}
