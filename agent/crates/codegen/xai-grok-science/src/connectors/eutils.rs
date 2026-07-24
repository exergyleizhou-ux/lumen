//! NCBI E-utilities shared transport. Disposition: protocol-partial-overlap.
//! PubMed Rust adapter hardcodes db=pubmed; no shared parameterizable adapter yet.
use super::adapter::ProtocolAdapter; use super::fetch::ParsedResponse; use crate::ScienceError;
pub struct EutilsAdapter;
impl ProtocolAdapter for EutilsAdapter { fn descriptor(&self) -> &'static super::ConnectorDescriptor { &super::EUTILS } fn expected_exchanges(&self) -> usize { 0 } fn build_fixture_paths(&self, _q: &str, _m: u32, _f: &[Vec<u8>]) -> crate::Result<Vec<String>> { Err(ScienceError::Invalid("eutils: protocol partially overlaps with PubMed; no shared db-parameterizable adapter exists".into())) } fn parse_responses(&self, _e: &[super::fetch::FetchExchange]) -> crate::Result<ParsedResponse> { Err(ScienceError::Invalid("eutils: not implemented".into())) } }
