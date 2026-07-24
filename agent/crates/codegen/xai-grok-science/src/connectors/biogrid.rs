//! BioGRID — rejected: unsafe (credential in URL). Disposition: rejected-unsafe-or-duplicate.
use super::adapter::ProtocolAdapter; use super::fetch::ParsedResponse; use crate::ScienceError;
pub struct BiogridAdapter;
impl ProtocolAdapter for BiogridAdapter { fn descriptor(&self) -> &'static super::ConnectorDescriptor { &super::BIOGRID_REJECTED } fn expected_exchanges(&self) -> usize { 0 } fn build_fixture_paths(&self, _q: &str, _m: u32, _f: &[Vec<u8>]) -> crate::Result<Vec<String>> { Err(ScienceError::Invalid("biogrid: rejected — credential in URL violates Lumen safety policy".into())) } fn parse_responses(&self, _e: &[super::fetch::FetchExchange]) -> crate::Result<ParsedResponse> { Err(ScienceError::Invalid("biogrid: rejected".into())) } }
