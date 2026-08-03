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
    pub fn is_authority_safe(self) -> bool {
        matches!(self, DeliveryObservationV1::Enqueued)
    }

    pub fn requires_recovery_or_freeze(self) -> bool {
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
}
