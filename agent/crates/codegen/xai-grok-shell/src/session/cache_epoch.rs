//! Durable, opaque cache-epoch metadata.
//!
//! This deliberately lives beside, rather than inside, `chat_history.jsonl`.
//! The record contains neither prompt material nor credentials: only an epoch
//! UUID and a digest of the effective cache domain.

use std::path::Path;

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use uuid::Uuid;

pub const CACHE_EPOCH_SCHEMA_VERSION: u32 = 1;
pub const CACHE_EPOCH_FILE_NAME: &str = "cache_epoch.json";
pub const CACHE_EVIDENCE_FILE_NAME: &str = "cache_request_evidence.jsonl";

/// Inputs that can alter provider-side cache identity. All fields are already
/// non-secret identities; callers must pass a credential slot/account ID, not
/// an API-key-derived value.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct CacheDomain {
    pub provider: String,
    pub base_url: String,
    pub backend: String,
    pub model: String,
    pub effective_effort: Option<String>,
    pub credential_scope: Option<String>,
    pub permission_domain: String,
    pub tool_manifest_fingerprint: String,
}

impl CacheDomain {
    pub fn fingerprint(&self) -> String {
        let bytes = serde_json::to_vec(self).expect("CacheDomain is serializable");
        let mut hash = Sha256::new();
        hash.update(bytes);
        format!("{:x}", hash.finalize())
    }
}

/// Hash an ordered, serializable semantic manifest. Unlike diagnostic-only
/// summaries, this deliberately preserves caller order because tool order is
/// part of the final provider request material.
pub fn ordered_manifest_fingerprint<T: Serialize>(value: &T) -> String {
    let bytes = serde_json::to_vec(value).expect("semantic manifest is serializable");
    let mut hash = Sha256::new();
    hash.update(bytes);
    format!("{:x}", hash.finalize())
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct CacheEpochRecord {
    pub schema_version: u32,
    pub epoch_id: Uuid,
    pub generation: u64,
    pub domain_fingerprint: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CacheEpochDisposition {
    Retained,
    CreatedMissing,
    RotatedDomainChanged,
    RotatedInvalidRecord,
    RotatedFork,
}

/// Loads the session record or atomically creates a replacement. A fork is
/// always a fresh epoch even if it inherited a byte-identical transcript.
pub fn load_or_rotate(
    session_dir: &Path,
    domain: &CacheDomain,
    is_fork: bool,
) -> std::io::Result<(CacheEpochRecord, CacheEpochDisposition)> {
    std::fs::create_dir_all(session_dir)?;
    let path = session_dir.join(CACHE_EPOCH_FILE_NAME);
    let desired = domain.fingerprint();
    if !is_fork {
        match std::fs::read(&path) {
            Ok(bytes) => match serde_json::from_slice::<CacheEpochRecord>(&bytes) {
                Ok(record)
                    if record.schema_version == CACHE_EPOCH_SCHEMA_VERSION
                        && record.domain_fingerprint == desired =>
                {
                    return Ok((record, CacheEpochDisposition::Retained));
                }
                Ok(record) if record.schema_version == CACHE_EPOCH_SCHEMA_VERSION => {
                    return write_next(
                        &path,
                        record.generation,
                        desired,
                        CacheEpochDisposition::RotatedDomainChanged,
                    );
                }
                Ok(record) => {
                    return write_next(
                        &path,
                        record.generation,
                        desired,
                        CacheEpochDisposition::RotatedInvalidRecord,
                    );
                }
                Err(_) => {
                    return write_next(
                        &path,
                        0,
                        desired,
                        CacheEpochDisposition::RotatedInvalidRecord,
                    );
                }
            },
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => {
                return write_next(&path, 0, desired, CacheEpochDisposition::CreatedMissing);
            }
            Err(err) => return Err(err),
        }
    }
    let generation = std::fs::read(&path)
        .ok()
        .and_then(|bytes| serde_json::from_slice::<CacheEpochRecord>(&bytes).ok())
        .map(|record| record.generation)
        .unwrap_or(0);
    write_next(
        &path,
        generation,
        desired,
        CacheEpochDisposition::RotatedFork,
    )
}

/// Rotate after a durable history mutation. This intentionally does not
/// compare the prior fingerprint: a committed rewrite changes the provider
/// cache material even if the configuration domain stayed the same.
pub fn rotate_after_history_mutation(
    session_dir: &Path,
    domain: &CacheDomain,
) -> std::io::Result<CacheEpochRecord> {
    std::fs::create_dir_all(session_dir)?;
    let path = session_dir.join(CACHE_EPOCH_FILE_NAME);
    let generation = std::fs::read(&path)
        .ok()
        .and_then(|bytes| serde_json::from_slice::<CacheEpochRecord>(&bytes).ok())
        .map(|record| record.generation)
        .unwrap_or(0);
    write_next(
        &path,
        generation,
        domain.fingerprint(),
        CacheEpochDisposition::RotatedDomainChanged,
    )
    .map(|(record, _)| record)
}

fn write_next(
    path: &Path,
    previous_generation: u64,
    domain_fingerprint: String,
    disposition: CacheEpochDisposition,
) -> std::io::Result<(CacheEpochRecord, CacheEpochDisposition)> {
    let record = CacheEpochRecord {
        schema_version: CACHE_EPOCH_SCHEMA_VERSION,
        epoch_id: Uuid::new_v4(),
        generation: previous_generation.saturating_add(1),
        domain_fingerprint,
    };
    let bytes = serde_json::to_vec_pretty(&record).expect("CacheEpochRecord is serializable");
    let temp = path.with_extension("json.tmp");
    let mut file = std::fs::File::create(&temp)?;
    use std::io::Write;
    file.write_all(&bytes)?;
    file.sync_all()?;
    drop(file);
    std::fs::rename(temp, path)?;
    Ok((record, disposition))
}

/// Append sanitized evidence for an outbound provider request. The sampler
/// supplies only one-way hashes and request shape metadata, never prompt
/// bytes, credentials, request IDs, or headers. Failure is deliberately
/// returned to the caller so the observer can remain fail-open.
pub fn append_request_evidence(
    session_dir: &Path,
    snapshot: &lumen_discipline::WireRequestSnapshot,
) -> std::io::Result<()> {
    std::fs::create_dir_all(session_dir)?;
    let path = session_dir.join(CACHE_EVIDENCE_FILE_NAME);
    let mut line = serde_json::to_vec(snapshot).expect("wire snapshot is serializable");
    line.push(b'\n');
    let mut file = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)?;
    use std::io::Write;
    file.write_all(&line)?;
    file.sync_data()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn domain() -> CacheDomain {
        CacheDomain {
            provider: "xai".into(),
            base_url: "https://api.x.ai".into(),
            backend: "responses".into(),
            model: "grok".into(),
            effective_effort: Some("high".into()),
            credential_scope: Some("slot-a".into()),
            permission_domain: "allowlist-a".into(),
            tool_manifest_fingerprint: "tools-a".into(),
        }
    }

    #[test]
    fn restart_retains_only_identical_domain() {
        let dir = tempfile::tempdir().unwrap();
        let (first, why) = load_or_rotate(dir.path(), &domain(), false).unwrap();
        assert_eq!(why, CacheEpochDisposition::CreatedMissing);
        let (again, why) = load_or_rotate(dir.path(), &domain(), false).unwrap();
        assert_eq!(why, CacheEpochDisposition::Retained);
        assert_eq!(again, first);
        let mut changed = domain();
        changed.permission_domain = "allowlist-b".into();
        let (rotated, why) = load_or_rotate(dir.path(), &changed, false).unwrap();
        assert_eq!(why, CacheEpochDisposition::RotatedDomainChanged);
        assert_ne!(rotated.epoch_id, first.epoch_id);
        assert_eq!(rotated.generation, first.generation + 1);
    }

    #[test]
    fn fork_and_invalid_metadata_rotate() {
        let dir = tempfile::tempdir().unwrap();
        let (first, _) = load_or_rotate(dir.path(), &domain(), false).unwrap();
        let (forked, why) = load_or_rotate(dir.path(), &domain(), true).unwrap();
        assert_eq!(why, CacheEpochDisposition::RotatedFork);
        assert_ne!(forked.epoch_id, first.epoch_id);
        std::fs::write(dir.path().join(CACHE_EPOCH_FILE_NAME), b"not json").unwrap();
        let (_, why) = load_or_rotate(dir.path(), &domain(), false).unwrap();
        assert_eq!(why, CacheEpochDisposition::RotatedInvalidRecord);
    }

    #[test]
    fn every_cache_domain_dimension_changes_fingerprint() {
        let baseline = domain();
        let expected = baseline.fingerprint();
        let mut variants = Vec::new();
        for replacement in [
            CacheDomain {
                provider: "other".into(),
                ..baseline.clone()
            },
            CacheDomain {
                base_url: "https://proxy.example".into(),
                ..baseline.clone()
            },
            CacheDomain {
                backend: "chat".into(),
                ..baseline.clone()
            },
            CacheDomain {
                model: "other-model".into(),
                ..baseline.clone()
            },
            CacheDomain {
                effective_effort: None,
                ..baseline.clone()
            },
            CacheDomain {
                credential_scope: Some("slot-b".into()),
                ..baseline.clone()
            },
            CacheDomain {
                permission_domain: "allowlist-b".into(),
                ..baseline.clone()
            },
            CacheDomain {
                tool_manifest_fingerprint: "tools-b".into(),
                ..baseline.clone()
            },
        ] {
            variants.push(replacement);
        }
        assert!(
            variants
                .into_iter()
                .all(|domain| domain.fingerprint() != expected)
        );
    }

    #[test]
    fn committed_history_mutation_rotates_even_in_same_domain() {
        let dir = tempfile::tempdir().unwrap();
        let (first, _) = load_or_rotate(dir.path(), &domain(), false).unwrap();
        let second = rotate_after_history_mutation(dir.path(), &domain()).unwrap();
        assert_ne!(first.epoch_id, second.epoch_id);
        assert_eq!(second.generation, first.generation + 1);
    }

    #[test]
    fn ordered_manifest_fingerprint_preserves_order() {
        assert_ne!(
            ordered_manifest_fingerprint(&vec!["one", "two"]),
            ordered_manifest_fingerprint(&vec!["two", "one"])
        );
    }

    #[test]
    fn request_evidence_is_durable_jsonl_without_request_material() {
        let dir = tempfile::tempdir().unwrap();
        let snapshot = lumen_discipline::WireRequestSnapshot {
            cache_domain_hash: "domain-hash".into(),
            cache_epoch_id: "epoch-id".into(),
            transport_hash: "transport-hash".into(),
            provider_cache_material_hash: "material-hash".into(),
            body_bytes: 42,
            wire_common_prefix_bytes: None,
            serialization_kind: lumen_discipline::WireSerializationKind::Responses,
            mutation_reasons: vec![],
            attempt_index: 0,
        };
        append_request_evidence(dir.path(), &snapshot).unwrap();
        let evidence = std::fs::read_to_string(dir.path().join(CACHE_EVIDENCE_FILE_NAME)).unwrap();
        let value: serde_json::Value = serde_json::from_str(&evidence).unwrap();
        assert_eq!(value["cache_epoch_id"], "epoch-id");
        assert_eq!(value["serialization_kind"], "responses");
        assert!(!evidence.contains("private prompt"));
    }
}
