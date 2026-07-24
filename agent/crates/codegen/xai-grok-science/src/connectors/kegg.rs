//! KEGG — pending license review. Disposition: license-review-required.
use super::adapter::ProtocolAdapter; use super::fetch::ParsedResponse; use crate::ScienceError;
pub struct KeggAdapter;
impl ProtocolAdapter for KeggAdapter { fn descriptor(&self) -> &'static super::ConnectorDescriptor { &super::KEGG_PENDING } fn expected_exchanges(&self) -> usize { 0 } fn build_fixture_paths(&self, _q: &str, _m: u32, _f: &[Vec<u8>]) -> crate::Result<Vec<String>> { Err(ScienceError::Invalid("kegg: pending license review — commercial use requires paid subscription".into())) } fn parse_responses(&self, _e: &[super::fetch::FetchExchange]) -> crate::Result<ParsedResponse> { Err(ScienceError::Invalid("kegg: pending license review".into())) } }
