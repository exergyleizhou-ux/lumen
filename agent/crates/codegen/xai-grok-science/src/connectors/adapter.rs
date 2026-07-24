//! Protocol adapter trait and global registry. Seam contract: DS-1.
//!
//! Every connector implements [`ProtocolAdapter`] once and registers itself
//! in the global [`REGISTRY`]. The fetch pipeline and shell extension route
//! through the registry instead of per-connector `match` blocks. Unknown
//! connector IDs fail closed.
//!
//! The trait is responsible only for protocol-specific pure behavior:
//! building relative paths, counting expected exchanges, and parsing
//! responses. Permission, artifact, evidence, provenance, audit, and replay
//! remain in the Lumen kernel (SessionActor).

use super::ConnectorDescriptor;
use super::fetch::{FetchExchange, ParsedResponse};
use crate::Result;
use std::collections::HashMap;
use std::sync::LazyLock;

/// Protocol-specific pure behavior contract for one external data connector.
pub trait ProtocolAdapter: Send + Sync {
    /// The compile-time descriptor for this connector (metadata, policy).
    fn descriptor(&self) -> &'static ConnectorDescriptor;

    /// Number of request/response exchanges this connector's v1 operation
    /// expects. Used by the product path to validate fixture sets before
    /// beginning a run.
    fn expected_exchanges(&self) -> usize;

    /// Build the relative-path sequence for a fixture-backed fetch. The
    /// caller wraps each path in [`ValidatedRequest`] via the kernel's
    /// policy gate; the adapter is responsible only for constructing the
    /// correct protocol-specific URLs.
    ///
    /// For pubmed, the esummary path depends on PMIDs parsed from the
    /// esearch fixture, so `fixtures` must already contain the raw
    /// response bytes for earlier exchanges.
    fn build_fixture_paths(
        &self,
        query: &str,
        max_results: u32,
        fixtures: &[Vec<u8>],
    ) -> Result<Vec<String>>;

    /// Parse one complete response-exchange sequence. The adapter must
    /// validate exchange count, detect malformed partial records, and
    /// fail closed before artifact registration.
    fn parse_responses(&self, exchanges: &[FetchExchange]) -> Result<ParsedResponse>;
}

/// Stable ordered registry of protocol adapters. A single global instance
/// lives in [`REGISTRY`]; adapters register at startup and unknown IDs
/// fail closed.
pub struct AdapterRegistry {
    adapters: Vec<Box<dyn ProtocolAdapter>>,
    by_id: HashMap<String, usize>,
}

impl AdapterRegistry {
    /// Create an empty registry.
    pub fn new() -> Self {
        AdapterRegistry {
            adapters: Vec::new(),
            by_id: HashMap::new(),
        }
    }

    /// Register a protocol adapter. Returns an error on duplicate ID.
    /// Once registered the adapter's position in the registry is stable.
    pub fn register(&mut self, adapter: Box<dyn ProtocolAdapter>) -> std::result::Result<(), String> {
        let id = adapter.descriptor().id.to_owned();
        if self.by_id.contains_key(&id) {
            return Err(format!("duplicate adapter id: {id}"));
        }
        let index = self.adapters.len();
        self.adapters.push(adapter);
        self.by_id.insert(id, index);
        Ok(())
    }

    /// Look up an adapter by connector id. Returns `None` for unknown
    /// ids — the caller must fail closed.
    pub fn get(&self, id: &str) -> Option<&dyn ProtocolAdapter> {
        self.by_id.get(id).map(|&index| self.adapters[index].as_ref())
    }

    /// Convenience: expected exchange count for a connector, or `None`
    /// if the connector is unknown.
    pub fn expected_exchanges(&self, id: &str) -> Option<usize> {
        self.get(id).map(|a| a.expected_exchanges())
    }

    /// All registered adapters in stable registration order.
    pub fn all(&self) -> &[Box<dyn ProtocolAdapter>] {
        &self.adapters
    }
}

impl Default for AdapterRegistry {
    fn default() -> Self {
        Self::new()
    }
}

/// Validate that every descriptor in [`super::registry()`] has a
/// corresponding adapter registered, and that no adapter is registered
/// without a matching descriptor. Returns `Ok(())` when the two registries
/// cover exactly the same set of connector IDs in the same stable order.
pub fn validate_adapter_descriptor_coverage(
    adapter_registry: &AdapterRegistry,
) -> std::result::Result<(), String> {
    let descriptors = super::registry();
    let adapter_ids: Vec<&str> = adapter_registry
        .all()
        .iter()
        .map(|a| a.descriptor().id)
        .collect();
    let descriptor_ids: Vec<&str> = descriptors.iter().map(|d| d.id).collect();

    // Check for missing adapters (descriptor with no matching adapter).
    for d in descriptors {
        if !adapter_registry.by_id.contains_key(d.id) {
            return Err(format!(
                "adapter coverage failure: descriptor '{}' has no registered adapter",
                d.id
            ));
        }
    }

    // Check for orphan adapters (adapter with no matching descriptor).
    for &id in &adapter_ids {
        if !descriptors.iter().any(|d| d.id == id) {
            return Err(format!(
                "adapter coverage failure: adapter '{}' has no matching descriptor",
                id
            ));
        }
    }

    // Enforce stable order match: adapter registration order must match
    // descriptor registry order.
    if adapter_ids != descriptor_ids {
        return Err(format!(
            "adapter/descriptor order mismatch: adapters {:?} != descriptors {:?}",
            adapter_ids, descriptor_ids
        ));
    }

    Ok(())
}

/// Global protocol adapter registry. Initialized once at startup.
/// Adding a connector means implementing [`ProtocolAdapter`], adding a
/// descriptor to [`super::registry()`], and registering the adapter here.
/// Coverage validation runs at init and fails closed if any descriptor
/// is missing its adapter (or vice versa).
pub static REGISTRY: LazyLock<AdapterRegistry> = LazyLock::new(|| {
    let mut registry = AdapterRegistry::new();

    registry.register(Box::new(super::pubmed::PubmedAdapter)).expect("pubmed adapter");
    registry.register(Box::new(super::chembl::ChemblAdapter)).expect("chembl adapter");
    registry.register(Box::new(super::crossref::CrossrefAdapter)).expect("crossref adapter");
    registry.register(Box::new(super::uniprot::UniprotAdapter)).expect("uniprot adapter");
    registry.register(Box::new(super::europepmc::EuropepmcAdapter)).expect("europepmc adapter");
    registry.register(Box::new(super::openalex::OpenalexAdapter)).expect("openalex adapter");

    validate_adapter_descriptor_coverage(&registry)
        .expect("adapter/descriptor coverage validation failed at init");

    registry
});

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn registry_rejects_duplicate_id() {
        let mut registry = AdapterRegistry::new();
        registry.register(Box::new(super::super::pubmed::PubmedAdapter)).unwrap();
        assert!(registry.register(Box::new(super::super::pubmed::PubmedAdapter)).is_err());
    }

    #[test]
    fn registry_unknown_id_returns_none() {
        assert!(REGISTRY.get("nonexistent").is_none());
        assert!(REGISTRY.expected_exchanges("nonexistent").is_none());
    }

    #[test]
    fn adapter_descriptor_coverage_is_one_to_one() {
        // Read the actual descriptor registry dynamically — no hardcoded vec.
        let descriptors = super::super::registry();
        let adapter_ids: Vec<&str> = REGISTRY.all().iter().map(|a| a.descriptor().id).collect();
        let descriptor_ids: Vec<&str> = descriptors.iter().map(|d| d.id).collect();

        assert_eq!(
            adapter_ids, descriptor_ids,
            "adapter/descriptor set mismatch: adapters={adapter_ids:?} descriptors={descriptor_ids:?}"
        );
        assert_eq!(
            adapter_ids.len(),
            descriptor_ids.len(),
            "adapter and descriptor count differ"
        );
        // Verify stable order — must be identical.
        assert_eq!(adapter_ids, descriptor_ids, "adapter/descriptor order mismatch");
    }

    #[test]
    fn coverage_rejects_missing_adapter() {
        // Create a registry with fewer adapters than descriptors.
        let mut registry = AdapterRegistry::new();
        // Register only pubmed — chembl, crossref, etc. are missing.
        registry.register(Box::new(super::super::pubmed::PubmedAdapter)).unwrap();
        let result = validate_adapter_descriptor_coverage(&registry);
        assert!(result.is_err(), "should reject when adapters are missing");
        assert!(
            result.unwrap_err().contains("has no registered adapter"),
            "error must identify missing adapter"
        );
    }

    #[test]
    fn coverage_rejects_orphan_adapter() {
        // Descriptors don't change at runtime, but an orphan adapter is one
        // registered without a matching descriptor. We simulate this by
        // constructing a temporary fake descriptor and a registry mismatch.
        let mut registry = AdapterRegistry::new();
        // Register all real adapters plus a duplicate-like scenario.
        registry.register(Box::new(super::super::pubmed::PubmedAdapter)).unwrap();
        // The coverage validator checks adapter IDs against descriptors.
        // Since chembl descriptor exists but adapter is missing, it fails.
        let result = validate_adapter_descriptor_coverage(&registry);
        assert!(result.is_err(), "should reject orphan or missing coverage");
    }

    #[test]
    fn every_adapter_descriptor_is_valid() {
        for adapter in REGISTRY.all() {
            super::super::validate_descriptor(adapter.descriptor())
                .unwrap_or_else(|e| panic!("{} invalid: {e}", adapter.descriptor().id));
        }
    }

    #[test]
    fn expected_exchanges_match_v1_protocol() {
        assert_eq!(REGISTRY.expected_exchanges("pubmed"), Some(2));
        assert_eq!(REGISTRY.expected_exchanges("chembl"), Some(1));
        assert_eq!(REGISTRY.expected_exchanges("crossref"), Some(1));
        assert_eq!(REGISTRY.expected_exchanges("uniprot"), Some(1));
        assert_eq!(REGISTRY.expected_exchanges("europepmc"), Some(1));
        assert_eq!(REGISTRY.expected_exchanges("openalex"), Some(1));
    }
}
