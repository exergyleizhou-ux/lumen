//! NG-03E / S4 — delivery observation for authority-sensitive sends.
//!
//! INV-18: authority events cannot be silently dropped. A bounded channel
//! `try_send` outcome must become a typed observation so callers can freeze
//! or surface RecoveryRequired instead of pretending delivery succeeded.
//!
//! This module is pure: it maps transport outcomes to observations. Wiring
//! into each mailbox is incremental; the type and mapping are the contract.

use serde::{Deserialize, Serialize};

/// What happened when an authority-sensitive event was offered to a queue.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum DeliveryObservationV1 {
    /// Accepted into a bounded queue (or unbounded send succeeded).
    Enqueued,
    /// Non-authority UI payload was merged/dropped under explicit policy.
    Coalesced { kept: u32, dropped: u32 },
    /// Queue rejected because it was full (backpressure).
    DroppedFull,
    /// Consumer gone / channel closed.
    ReceiverClosed,
    /// Transport result was ambiguous (e.g. timeout before ack).
    Unknown { reason: String },
}

impl DeliveryObservationV1 {
    /// Authority-safe path: only Enqueued is "delivered enough to continue
    /// without a delivery observation gate". Coalesced is allowed only for
    /// non-authority UI (callers must not use it for grants/leases/receipts).
    pub fn is_authority_safe(&self) -> bool {
        matches!(self, DeliveryObservationV1::Enqueued)
    }

    pub fn requires_recovery_or_freeze(&self) -> bool {
        matches!(
            self,
            DeliveryObservationV1::DroppedFull
                | DeliveryObservationV1::ReceiverClosed
                | DeliveryObservationV1::Unknown { .. }
        )
    }
}

/// Map a std/tokio-style try-send error into an observation.
///
/// Callers pass `full` vs `disconnected` so we stay free of a hard dependency
/// on a particular channel crate in the public signature.
pub fn observation_from_try_send(result: Result<(), TrySendKind>) -> DeliveryObservationV1 {
    match result {
        Ok(()) => DeliveryObservationV1::Enqueued,
        Err(TrySendKind::Full) => DeliveryObservationV1::DroppedFull,
        Err(TrySendKind::Disconnected) => DeliveryObservationV1::ReceiverClosed,
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TrySendKind {
    Full,
    Disconnected,
}

/// Observe a real `std::sync::mpsc::SyncSender::try_send` outcome.
pub fn observe_std_sync_try_send<T>(
    result: Result<(), std::sync::mpsc::TrySendError<T>>,
) -> DeliveryObservationV1 {
    match result {
        Ok(()) => observation_from_try_send(Ok(())),
        Err(std::sync::mpsc::TrySendError::Full(_)) => {
            observation_from_try_send(Err(TrySendKind::Full))
        }
        Err(std::sync::mpsc::TrySendError::Disconnected(_)) => {
            observation_from_try_send(Err(TrySendKind::Disconnected))
        }
    }
}

/// Bounded-queue pressure sample (capacity / depth). Pure accounting for
/// fair-share and backpressure UI — not authority by itself.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub struct QueuePressureV1 {
    pub capacity: u32,
    pub depth: u32,
    pub high_watermark: u32,
}

impl QueuePressureV1 {
    pub fn new(capacity: u32, depth: u32) -> Self {
        let high_watermark = capacity.saturating_mul(80).saturating_div(100).max(1);
        Self {
            capacity,
            depth: depth.min(capacity),
            high_watermark,
        }
    }

    pub fn utilization_bps(self) -> u32 {
        if self.capacity == 0 {
            return 10_000;
        }
        (u64::from(self.depth) * 10_000 / u64::from(self.capacity)) as u32
    }

    pub fn is_high(self) -> bool {
        self.depth >= self.high_watermark
    }

    pub fn is_saturated(self) -> bool {
        self.capacity > 0 && self.depth >= self.capacity
    }
}

/// Combine a try-send observation with queue pressure for operator projection.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct DeliverySampleV1 {
    pub observation: DeliveryObservationV1,
    pub pressure: QueuePressureV1,
}

impl DeliverySampleV1 {
    pub fn from_try_send(
        result: Result<(), TrySendKind>,
        capacity: u32,
        depth_before_send: u32,
    ) -> Self {
        let observation = observation_from_try_send(result);
        let depth = match observation {
            DeliveryObservationV1::Enqueued => depth_before_send.saturating_add(1),
            _ => depth_before_send,
        };
        Self {
            observation,
            pressure: QueuePressureV1::new(capacity, depth.min(capacity)),
        }
    }

    /// Authority path: full/closed/unknown → freeze; high pressure alone is
    /// warning, not freeze (INV-18 needs delivery failure, not just load).
    pub fn authority_disposition(&self) -> AuthorityDeliveryDisposition {
        if self.observation.requires_recovery_or_freeze() {
            AuthorityDeliveryDisposition::FreezeOrRecover
        } else if self.pressure.is_high() {
            AuthorityDeliveryDisposition::WarnPressure
        } else {
            AuthorityDeliveryDisposition::Continue
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum AuthorityDeliveryDisposition {
    Continue,
    WarnPressure,
    FreezeOrRecover,
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::mpsc;

    #[test]
    fn real_sync_channel_full_and_disconnected_are_observed() {
        let (tx, rx) = mpsc::sync_channel::<u32>(1);
        assert_eq!(
            observe_std_sync_try_send(tx.try_send(1)),
            DeliveryObservationV1::Enqueued
        );
        // Capacity 1 already held → Full.
        assert_eq!(
            observe_std_sync_try_send(tx.try_send(2)),
            DeliveryObservationV1::DroppedFull
        );
        drop(rx);
        assert_eq!(
            observe_std_sync_try_send(tx.try_send(3)),
            DeliveryObservationV1::ReceiverClosed
        );
        assert!(DeliveryObservationV1::DroppedFull.requires_recovery_or_freeze());
        assert!(!DeliveryObservationV1::Enqueued.requires_recovery_or_freeze());
        assert!(DeliveryObservationV1::Enqueued.is_authority_safe());
        assert!(!DeliveryObservationV1::Coalesced {
            kept: 1,
            dropped: 9
        }
        .is_authority_safe());
    }

    #[test]
    fn queue_pressure_and_sample_drive_disposition() {
        let ok = DeliverySampleV1::from_try_send(Ok(()), 10, 2);
        assert_eq!(ok.observation, DeliveryObservationV1::Enqueued);
        assert_eq!(ok.pressure.depth, 3);
        assert_eq!(
            ok.authority_disposition(),
            AuthorityDeliveryDisposition::Continue
        );

        let high = DeliverySampleV1 {
            observation: DeliveryObservationV1::Enqueued,
            pressure: QueuePressureV1::new(10, 9),
        };
        assert!(high.pressure.is_high());
        assert_eq!(
            high.authority_disposition(),
            AuthorityDeliveryDisposition::WarnPressure
        );

        let full = DeliverySampleV1::from_try_send(Err(TrySendKind::Full), 4, 4);
        assert_eq!(full.observation, DeliveryObservationV1::DroppedFull);
        assert_eq!(
            full.authority_disposition(),
            AuthorityDeliveryDisposition::FreezeOrRecover
        );
        assert!(full.pressure.is_saturated());
    }

    /// Token-flood fixture: a bounded authority mailbox of capacity N refuses
    /// the (N+1)th try_send with DroppedFull → FreezeOrRecover (not silent drop).
    #[test]
    fn token_flood_fixture_refuses_overflow_with_freeze() {
        const CAP: usize = 8;
        let (tx, _rx) = mpsc::sync_channel::<&'static str>(CAP);
        let mut enqueued = 0u32;
        let mut froze = 0u32;
        for i in 0..(CAP + 4) {
            let obs = observe_std_sync_try_send(tx.try_send("token"));
            let sample = DeliverySampleV1 {
                observation: obs.clone(),
                pressure: QueuePressureV1::new(CAP as u32, enqueued.min(CAP as u32)),
            };
            match sample.authority_disposition() {
                AuthorityDeliveryDisposition::Continue
                | AuthorityDeliveryDisposition::WarnPressure => {
                    assert_eq!(obs, DeliveryObservationV1::Enqueued);
                    enqueued += 1;
                }
                AuthorityDeliveryDisposition::FreezeOrRecover => {
                    assert!(matches!(
                        obs,
                        DeliveryObservationV1::DroppedFull
                    ));
                    froze += 1;
                    assert!(i >= CAP, "overflow must not start before capacity");
                }
            }
        }
        assert_eq!(enqueued, CAP as u32);
        assert_eq!(froze, 4);
    }

    /// Two-tree fairness: each tree has its own bounded mailbox; flood on tree
    /// A must not fill tree B's capacity (isolated SyncSender per tree).
    #[test]
    fn two_tree_fairness_fixture_isolates_mailbox_pressure() {
        let (tx_a, _rx_a) = mpsc::sync_channel::<u8>(2);
        let (tx_b, rx_b) = mpsc::sync_channel::<u8>(2);
        // Flood A to saturation.
        assert_eq!(
            observe_std_sync_try_send(tx_a.try_send(1)),
            DeliveryObservationV1::Enqueued
        );
        assert_eq!(
            observe_std_sync_try_send(tx_a.try_send(2)),
            DeliveryObservationV1::Enqueued
        );
        assert_eq!(
            observe_std_sync_try_send(tx_a.try_send(3)),
            DeliveryObservationV1::DroppedFull
        );
        // B still accepts its own budget.
        assert_eq!(
            observe_std_sync_try_send(tx_b.try_send(10)),
            DeliveryObservationV1::Enqueued
        );
        assert_eq!(
            observe_std_sync_try_send(tx_b.try_send(11)),
            DeliveryObservationV1::Enqueued
        );
        assert_eq!(rx_b.try_recv().ok(), Some(10));
    }

    /// Shutdown drain: after drop(receiver), further try_send is ReceiverClosed
    /// (fail-closed); in-flight items already taken are not re-offered.
    #[test]
    fn shutdown_drain_fixture_closes_without_silent_loss_of_policy() {
        let (tx, rx) = mpsc::sync_channel::<&'static str>(4);
        assert_eq!(
            observe_std_sync_try_send(tx.try_send("a")),
            DeliveryObservationV1::Enqueued
        );
        assert_eq!(
            observe_std_sync_try_send(tx.try_send("b")),
            DeliveryObservationV1::Enqueued
        );
        // Drain what was enqueued.
        let mut drained = Vec::new();
        while let Ok(v) = rx.try_recv() {
            drained.push(v);
        }
        assert_eq!(drained, ["a", "b"]);
        drop(rx);
        let closed = observe_std_sync_try_send(tx.try_send("late"));
        assert_eq!(closed, DeliveryObservationV1::ReceiverClosed);
        assert!(closed.requires_recovery_or_freeze());
        assert!(!closed.is_authority_safe());
    }
}
