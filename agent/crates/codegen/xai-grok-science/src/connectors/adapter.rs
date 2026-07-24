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

/// Global protocol adapter registry. Initialized once at startup.
/// Adding a connector means implementing [`ProtocolAdapter`] and
/// registering it here.
pub static REGISTRY: LazyLock<AdapterRegistry> = LazyLock::new(|| {
    let mut registry = AdapterRegistry::new();

    registry.register(Box::new(super::pubmed::PubmedAdapter)).expect("pubmed adapter");
    registry.register(Box::new(super::chembl::ChemblAdapter)).expect("chembl adapter");
    registry.register(Box::new(super::crossref::CrossrefAdapter)).expect("crossref adapter");
    registry.register(Box::new(super::uniprot::UniprotAdapter)).expect("uniprot adapter");
    registry.register(Box::new(super::europepmc::EuropepmcAdapter)).expect("europepmc adapter");
    registry.register(Box::new(super::openalex::OpenalexAdapter)).expect("openalex adapter");

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
    fn registry_has_all_six_connectors() {
        let ids: Vec<&str> = REGISTRY.all().iter().map(|a| a.descriptor().id).collect();
        assert_eq!(ids, vec!["pubmed", "chembl", "crossref", "uniprot", "europepmc", "openalex"]);
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
