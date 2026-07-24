//! Provider-neutral, privacy-preserving evidence for an actual outbound request.
//!
//! This module intentionally contains no HTTP client or prompt bytes. The
//! sampler owns request building and supplies only sanitized, one-way evidence.

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum WireSerializationKind {
    ChatCompletions,
    Responses,
    Messages,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum WireMutationReason {
    RetryImageStrip,
    ImageEvicted,
    ToolResultPruned,
    MemoryChanged,
    FullCompaction,
    ModelChanged,
    BaseUrlChanged,
    PermissionProfileChanged,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct WireRequestSnapshot {
    pub cache_domain_hash: String,
    pub cache_epoch_id: String,
    pub transport_hash: String,
    pub provider_cache_material_hash: String,
    pub body_bytes: u64,
    /// Available only while the predecessor bytes are retained in memory.
    pub wire_common_prefix_bytes: Option<u64>,
    pub serialization_kind: WireSerializationKind,
    pub mutation_reasons: Vec<WireMutationReason>,
    pub attempt_index: u32,
}

/// Dynamic, session-owned inputs for one sampling submission. The sampler
/// derives the attempt index inside its retry loop; it must never be captured
/// in a long-lived `SamplerConfig`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WireObservationContext {
    pub cache_domain_hash: String,
    pub cache_epoch_id: String,
    pub mutation_reasons: Vec<WireMutationReason>,
}

/// Hash a byte sequence without retaining it. This is suitable for sanitized
/// request material, never for identity secrets or raw credentials.
pub fn digest_hex(bytes: &[u8]) -> String {
    Sha256::digest(bytes)
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect()
}

/// Exact common-prefix count for ephemeral in-memory diagnostics. Callers must
/// discard both byte slices before persistence/restart.
pub fn common_prefix_bytes(previous: &[u8], current: &[u8]) -> u64 {
    previous
        .iter()
        .zip(current)
        .take_while(|(left, right)| left == right)
        .count() as u64
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn prefix_is_ephemeral_and_exact() {
        assert_eq!(common_prefix_bytes(b"abc-one", b"abc-two"), 4);
        assert_eq!(common_prefix_bytes(b"", b"x"), 0);
    }

    #[test]
    fn digest_does_not_return_input() {
        let digest = digest_hex(b"sensitive prompt");
        assert_eq!(digest.len(), 64);
        assert!(!digest.contains("sensitive"));
    }
}
