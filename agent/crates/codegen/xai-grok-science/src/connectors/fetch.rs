//! Connector fetch run protocol. Seam contract: S3.
//!
//! One fetch run models one complete connector operation as a sequence of
//! request/response exchanges (two for pubmed esearch+esummary, one for a
//! ChEMBL, Crossref, UniProt, or Europe PMC search). The responses reach this module as
//! bytes that transited Lumen's formal workspace tool dispatch; the kernel re-parses every
//! exchange and fails the run closed before registering any artifact when a
//! response is malformed. Credentials never appear here: the request URLs
//! contain only public query terms, the redacted audit hashes each URL, and
//! the scientific evidence cites the public source URL as its citation.

use super::{ConnectorAudit, ConnectorOutcome, ValidatedRequest, connector_audit, descriptor};
use crate::{
    Approval, ApprovalDecision, Artifact, CallId, Evidence, Provenance, Result, RunContext,
    RunRecord, RunState, ScienceError, ScienceStore,
    csv::{self, ScienceRunTicket},
};
use chrono::Utc;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{collections::BTreeMap, path::Path};

/// One normalized record from any connector, with its citation URL.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct RetrievedRecord {
    /// Stable external id (PMID, ChEMBL id).
    pub id: String,
    pub title: String,
    /// Journal, database, or collection the record belongs to.
    pub container: String,
    /// Canonical public URL for the record.
    pub url: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ParsedResponse {
    pub total_hits: u64,
    pub records: Vec<RetrievedRecord>,
}

/// One request/response exchange. The response bytes must be exactly what
/// the formal tool dispatch produced; the kernel never trusts a caller's
/// parse of them.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FetchExchange {
    pub request: ValidatedRequest,
    pub response: Vec<u8>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct FetchResult {
    pub run: RunRecord,
    pub artifacts: Vec<Artifact>,
    pub evidence: Vec<Evidence>,
    pub provenance: Vec<Provenance>,
    pub approvals: Vec<Approval>,
    /// Redacted per-exchange audit records (URL and response by hash only).
    pub audits: Vec<ConnectorAudit>,
    /// Mandatory service notice for the caller to display alongside results.
    pub user_notice: String,
    pub parsed: ParsedResponse,
    pub replay_after: u64,
}

/// Parse the exchanges of a completed fetch for `connector_id`. Exchange
/// count and order are fixed per connector protocol.
pub fn parse_responses(connector_id: &str, exchanges: &[FetchExchange]) -> Result<ParsedResponse> {
    match connector_id {
        "pubmed" => {
            if exchanges.len() != 2 {
                return Err(ScienceError::Invalid(
                    "pubmed fetch requires esearch and esummary exchanges".into(),
                ));
            }
            let (total_hits, _ids) = super::pubmed::parse_esearch(&exchanges[0].response)?;
            let records = super::pubmed::parse_esummary(&exchanges[1].response)?;
            Ok(ParsedResponse {
                total_hits,
                records,
            })
        }
        "chembl" => {
            if exchanges.len() != 1 {
                return Err(ScienceError::Invalid(
                    "chembl fetch requires exactly one search exchange".into(),
                ));
            }
            super::chembl::parse_search(&exchanges[0].response)
        }
        "crossref" => {
            if exchanges.len() != 1 {
                return Err(ScienceError::Invalid(
                    "crossref fetch requires exactly one works exchange".into(),
                ));
            }
            super::crossref::parse_works(&exchanges[0].response)
        }
        "uniprot" => {
            if exchanges.len() != 1 {
                return Err(ScienceError::Invalid(
                    "uniprot fetch requires exactly one search exchange".into(),
                ));
            }
            super::uniprot::parse_search(&exchanges[0].response)
        }
        "europepmc" => {
            if exchanges.len() != 1 {
                return Err(ScienceError::Invalid(
                    "europepmc fetch requires exactly one search exchange".into(),
                ));
            }
            super::europepmc::parse_search(&exchanges[0].response)
        }
        other => Err(ScienceError::Invalid(format!(
            "no protocol adapter for connector: {other}"
        ))),
    }
}

/// Expected exchange count for a connector's v1 operation, used by the
/// product path to validate fixture sets before beginning a run.
pub fn expected_exchanges(connector_id: &str) -> Option<usize> {
    match connector_id {
        "pubmed" => Some(2),
        "chembl" => Some(1),
        "crossref" => Some(1),
        "uniprot" | "europepmc" => Some(1),
        _ => None,
    }
}

/// Phase one of the fetch protocol, mirroring the CSV/import loops.
pub fn begin_fetch(store: &ScienceStore, context: RunContext) -> Result<ScienceRunTicket> {
    let ticket = ScienceRunTicket {
        project_id: context.project_id.clone(),
        run_id: context.run_id.clone(),
        owner_id: context.owner_id.clone(),
        call_id: CallId::new("science_connector_fetch"),
    };
    store.create_run(context)?;
    store.append_event(
        &ticket.run_id,
        "SessionActor",
        "run.created",
        serde_json::json!({}),
    )?;
    store.request_approval(Approval {
        project_id: ticket.project_id.clone(),
        run_id: ticket.run_id.clone(),
        call_id: ticket.call_id.clone(),
        owner_id: ticket.owner_id.clone(),
        decision: ApprovalDecision::Pending,
        decided_at: None,
    })?;
    store.transition(&ticket.run_id, RunState::AwaitingApproval, None)?;
    Ok(ticket)
}

/// Complete an allowed fetch run. Re-parses every exchange; a malformed
/// response fails the run closed with no artifacts registered.
pub fn finish_fetch(
    store: &ScienceStore,
    ticket: ScienceRunTicket,
    connector_id: &str,
    query: &str,
    exchanges: Vec<FetchExchange>,
    tool_identity: impl Into<String>,
) -> Result<FetchResult> {
    let run = store.load_run(&ticket.run_id)?;
    if run.state != RunState::Running
        || store
            .approvals(&ticket.run_id)?
            .iter()
            .find(|approval| approval.call_id == ticket.call_id)
            .is_none_or(|approval| approval.decision != ApprovalDecision::Allow)
    {
        return Err(ScienceError::Invalid(
            "connector fetch requires an allowed running run".into(),
        ));
    }
    let descriptor = descriptor(connector_id)
        .ok_or_else(|| ScienceError::Invalid(format!("unknown connector: {connector_id}")))?;
    if exchanges.is_empty() {
        return Err(ScienceError::Invalid(
            "connector fetch requires at least one exchange".into(),
        ));
    }
    let parsed = match parse_responses(connector_id, &exchanges) {
        Ok(parsed) => parsed,
        Err(error) => {
            let reason = format!("connector response failed closed: {error}");
            let _ = store.transition(&ticket.run_id, RunState::Failed, Some(reason.clone()));
            return Err(ScienceError::Invalid(reason));
        }
    };
    let tool_identity = tool_identity.into();
    let mut artifacts = Vec::with_capacity(exchanges.len());
    let mut audits = Vec::with_capacity(exchanges.len());
    for (index, exchange) in exchanges.iter().enumerate() {
        let artifact = store.put_artifact(
            &ticket.project_id,
            &ticket.run_id,
            &ticket.owner_id,
            ticket.call_id.clone(),
            Path::new(&format!("response_{index}.json")),
            &exchange.response,
            "application/json",
            format!(
                "{} exchange {index}: {} bytes",
                connector_id,
                exchange.response.len()
            ),
        )?;
        artifacts.push(artifact);
        let audit = connector_audit(
            &exchange.request,
            Some(format!("{:x}", Sha256::digest(&exchange.response))),
            Utc::now().timestamp_millis(),
            ConnectorOutcome::Retrieved,
        );
        store.append_event(
            &ticket.run_id,
            "LumenConnector",
            "connector.exchange",
            serde_json::to_value(&audit).map_err(ScienceError::Serde)?,
        )?;
        audits.push(audit);
    }
    let source_url = exchanges[0].request.url.clone();
    let first = parsed.records.first();
    let claim = match first {
        Some(record) => format!(
            "{} search {query:?}: {} hits; first: {} ({}), {}",
            connector_id, parsed.total_hits, record.title, record.id, record.container
        ),
        None => format!(
            "{} search {query:?}: {} hits; no records",
            connector_id, parsed.total_hits
        ),
    };
    store.add_provenance(Provenance {
        run_id: ticket.run_id.clone(),
        source_uri: source_url.clone(),
        source_commit: None,
        source_path: None,
        license: descriptor.tos_url.to_owned(),
        retrieved_at: Utc::now(),
        input_sha256: format!("{:x}", Sha256::digest(&exchanges[0].response)),
        tool: tool_identity.clone(),
        environment: BTreeMap::from([
            ("connector".into(), connector_id.to_owned()),
            ("query".into(), query.to_owned()),
            ("data_class".into(), format!("{:?}", descriptor.data_class)),
        ]),
    })?;
    store.add_evidence(Evidence {
        run_id: ticket.run_id.clone(),
        claim,
        source: source_url,
        artifact_sha256: artifacts.first().map(|artifact| artifact.sha256.clone()),
        verified_at: Utc::now(),
    })?;
    store.append_event(
        &ticket.run_id,
        "LumenToolDispatch",
        "tool.completed",
        serde_json::json!({
            "tool": tool_identity,
            "artifacts": artifacts.iter().map(|artifact| artifact.sha256.clone()).collect::<Vec<_>>()
        }),
    )?;
    let run = store.transition(&ticket.run_id, RunState::Succeeded, None)?;
    store.append_event(
        &ticket.run_id,
        "HostVerification",
        "run.succeeded",
        serde_json::json!({}),
    )?;
    Ok(FetchResult {
        audits,
        user_notice: descriptor.user_notice.to_owned(),
        parsed,
        replay_after: store
            .events_after(&run.context.run_id, 0, 1_000)?
            .last()
            .map_or(0, |event| event.seq),
        artifacts: store.artifacts(&run.context.run_id)?,
        evidence: store.evidence(&run.context.run_id)?,
        provenance: store.provenance(&run.context.run_id)?,
        approvals: store.approvals(&run.context.run_id)?,
        run,
    })
}

/// Kernel-test convenience: begin, allow, and finish a fetch without the
/// product permission bridge. Product code must use the SessionActor path.
pub fn execute_approved_fetch(
    store: &ScienceStore,
    context: RunContext,
    connector_id: &str,
    query: &str,
    exchanges: Vec<FetchExchange>,
) -> Result<FetchResult> {
    let ticket = begin_fetch(store, context)?;
    csv::mark_allowed(store, &ticket)?;
    finish_fetch(
        store,
        ticket,
        connector_id,
        query,
        exchanges,
        "kernel-test-only/direct-executor",
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ProjectId;

    const ESEARCH: &[u8] =
        br#"{"esearchresult": {"count": "2", "idlist": ["41234567", "41234568"]}}"#;
    const ESUMMARY: &[u8] = br#"{"result": {"uids": ["41234567", "41234568"],
        "41234567": {"uid": "41234567", "title": "Base editing advances", "fulljournalname": "Nature"},
        "41234568": {"uid": "41234568", "title": "Prime editing review", "fulljournalname": "Cell"}}}"#;
    const CHEMBL: &[u8] =
        br#"{"molecules": [{"molecule_chembl_id": "CHEMBL25", "pref_name": "ASPIRIN"}],
        "page_meta": {"total_count": 1}}"#;
    const CROSSREF: &[u8] = br#"{"status":"ok","message":{"total-results":1,"items":[{
        "DOI":"10.5555/example.1","title":["Reproducible science workflows"],
        "container-title":["Journal of Durable Research"]}]}}"#;
    const UNIPROT: &[u8] = br#"{"results":[{"primaryAccession":"P01308",
        "uniProtkbId":"INS_HUMAN","proteinDescription":{"recommendedName":{"fullName":{"value":"Insulin"}}},
        "organism":{"scientificName":"Homo sapiens"}}],"totalResults":1}"#;
    const EUROPEPMC: &[u8] = br#"{"hitCount":1,"resultList":{"result":[{
        "id":"41234567","source":"MED","title":"Reproducible single-cell analysis",
        "journalTitle":"Genome Methods","pubYear":"2026"}]}}"#;

    fn exchange(connector: &str, path: &str, response: &[u8]) -> FetchExchange {
        FetchExchange {
            request: super::super::validate_request(connector, path, false, 5_000).unwrap(),
            response: response.to_vec(),
        }
    }

    fn pubmed_exchanges() -> Vec<FetchExchange> {
        vec![
            exchange(
                "pubmed",
                "/esearch.fcgi?db=pubmed&retmode=json&term=crispr",
                ESEARCH,
            ),
            exchange(
                "pubmed",
                "/esummary.fcgi?db=pubmed&retmode=json&id=41234567,41234568",
                ESUMMARY,
            ),
        ]
    }

    #[test]
    fn pubmed_fetch_records_citation_evidence_and_replays() {
        let temp = tempfile::tempdir().unwrap();
        let store = ScienceStore::new(temp.path());
        let result = execute_approved_fetch(
            &store,
            csv::fixture_context(temp.path(), ProjectId::new("p"), "alice"),
            "pubmed",
            "crispr",
            pubmed_exchanges(),
        )
        .unwrap();
        assert_eq!(result.parsed.total_hits, 2);
        assert_eq!(result.parsed.records[0].id, "41234567");
        assert_eq!(result.artifacts.len(), 2);
        assert_eq!(result.audits.len(), 2);
        assert!(result.user_notice.contains("NCBI disclaimer"));
        // Audit is redacted; evidence carries the scientific citation.
        assert!(!result.audits[0].request_sha256.contains("crispr"));
        assert!(result.evidence[0].claim.contains("crispr"));
        assert!(result.evidence[0].claim.contains("Base editing advances"));
        assert!(
            result.evidence[0]
                .source
                .contains("eutils.ncbi.nlm.nih.gov")
        );
        assert_eq!(
            result.evidence[0].artifact_sha256.as_deref(),
            Some(result.artifacts[0].sha256.as_str())
        );
        assert_eq!(
            result.provenance[0].license,
            descriptor("pubmed").unwrap().tos_url
        );
        let before = serde_json::to_value(&result).unwrap();
        drop(store);
        let reopened = ScienceStore::new(temp.path());
        let run = reopened.load_run(&result.run.context.run_id).unwrap();
        let replay = FetchResult {
            audits: result.audits.clone(),
            user_notice: result.user_notice.clone(),
            parsed: parse_responses(
                "pubmed",
                &[
                    exchange(
                        "pubmed",
                        "/esearch.fcgi?db=pubmed&retmode=json&term=crispr",
                        ESEARCH,
                    ),
                    exchange(
                        "pubmed",
                        "/esummary.fcgi?db=pubmed&retmode=json&id=41234567,41234568",
                        ESUMMARY,
                    ),
                ],
            )
            .unwrap(),
            replay_after: reopened
                .events_after(&run.context.run_id, 0, 1_000)
                .unwrap()
                .last()
                .map_or(0, |event| event.seq),
            artifacts: reopened.artifacts(&run.context.run_id).unwrap(),
            evidence: reopened.evidence(&run.context.run_id).unwrap(),
            provenance: reopened.provenance(&run.context.run_id).unwrap(),
            approvals: reopened.approvals(&run.context.run_id).unwrap(),
            run,
        };
        assert_eq!(before, serde_json::to_value(replay).unwrap());
    }

    #[test]
    fn chembl_fetch_single_exchange() {
        let temp = tempfile::tempdir().unwrap();
        let store = ScienceStore::new(temp.path());
        let result = execute_approved_fetch(
            &store,
            csv::fixture_context(temp.path(), ProjectId::new("p"), "alice"),
            "chembl",
            "aspirin",
            vec![exchange(
                "chembl",
                "/molecule/search.json?q=aspirin&limit=5&offset=0",
                CHEMBL,
            )],
        )
        .unwrap();
        assert_eq!(result.parsed.total_hits, 1);
        assert_eq!(result.parsed.records[0].id, "CHEMBL25");
        assert_eq!(result.artifacts.len(), 1);
    }

    #[test]
    fn crossref_fetch_records_notice_citation_and_replays() {
        let temp = tempfile::tempdir().unwrap();
        let store = ScienceStore::new(temp.path());
        let result = execute_approved_fetch(
            &store,
            csv::fixture_context(temp.path(), ProjectId::new("p"), "alice"),
            "crossref",
            "reproducible science",
            vec![exchange(
                "crossref",
                &super::super::crossref::works_path("reproducible science", 5),
                CROSSREF,
            )],
        )
        .unwrap();
        assert_eq!(result.parsed.total_hits, 1);
        assert_eq!(result.parsed.records[0].id, "10.5555/example.1");
        assert_eq!(
            result.parsed.records[0].title,
            "Reproducible science workflows"
        );
        assert!(result.user_notice.contains("no abstracts"));
        assert_eq!(result.artifacts.len(), 1);
        assert_eq!(result.audits.len(), 1);
        assert!(result.evidence[0].source.contains("api.crossref.org"));
        let before = serde_json::to_value(&result).unwrap();
        drop(store);
        let reopened = ScienceStore::new(temp.path());
        let run = reopened.load_run(&result.run.context.run_id).unwrap();
        let replay = FetchResult {
            run,
            artifacts: reopened.artifacts(&result.run.context.run_id).unwrap(),
            evidence: reopened.evidence(&result.run.context.run_id).unwrap(),
            provenance: reopened.provenance(&result.run.context.run_id).unwrap(),
            approvals: reopened.approvals(&result.run.context.run_id).unwrap(),
            audits: result.audits.clone(),
            user_notice: result.user_notice.clone(),
            parsed: parse_responses(
                "crossref",
                &[exchange(
                    "crossref",
                    &super::super::crossref::works_path("reproducible science", 5),
                    CROSSREF,
                )],
            )
            .unwrap(),
            replay_after: result.replay_after,
        };
        assert_eq!(before, serde_json::to_value(replay).unwrap());
    }

    #[test]
    fn uniprot_fetch_records_notice_citation_and_replays() {
        let temp = tempfile::tempdir().unwrap();
        let store = ScienceStore::new(temp.path());
        let result = execute_approved_fetch(
            &store,
            csv::fixture_context(temp.path(), ProjectId::new("p"), "alice"),
            "uniprot",
            "human insulin",
            vec![exchange(
                "uniprot",
                &super::super::uniprot::search_path("human insulin", 5),
                UNIPROT,
            )],
        )
        .unwrap();
        assert_eq!(result.parsed.total_hits, 1);
        assert_eq!(result.parsed.records[0].id, "P01308");
        assert_eq!(result.parsed.records[0].title, "Insulin");
        assert!(result.user_notice.contains("CC BY 4.0"));
        assert_eq!(result.artifacts.len(), 1);
        assert_eq!(result.audits.len(), 1);
        assert!(result.evidence[0].source.contains("rest.uniprot.org"));
        let before = serde_json::to_value(&result).unwrap();
        drop(store);
        let reopened = ScienceStore::new(temp.path());
        let replay = FetchResult {
            run: reopened.load_run(&result.run.context.run_id).unwrap(),
            artifacts: reopened.artifacts(&result.run.context.run_id).unwrap(),
            evidence: reopened.evidence(&result.run.context.run_id).unwrap(),
            provenance: reopened.provenance(&result.run.context.run_id).unwrap(),
            approvals: reopened.approvals(&result.run.context.run_id).unwrap(),
            audits: result.audits.clone(),
            user_notice: result.user_notice.clone(),
            parsed: parse_responses(
                "uniprot",
                &[exchange(
                    "uniprot",
                    &super::super::uniprot::search_path("human insulin", 5),
                    UNIPROT,
                )],
            )
            .unwrap(),
            replay_after: result.replay_after,
        };
        assert_eq!(before, serde_json::to_value(replay).unwrap());
    }

    #[test]
    fn europepmc_fetch_records_notice_citation_and_replays() {
        let temp = tempfile::tempdir().unwrap();
        let store = ScienceStore::new(temp.path());
        let result = execute_approved_fetch(
            &store,
            csv::fixture_context(temp.path(), ProjectId::new("p"), "alice"),
            "europepmc",
            "single cell RNA",
            vec![exchange(
                "europepmc",
                &super::super::europepmc::search_path("single cell RNA", 5),
                EUROPEPMC,
            )],
        )
        .unwrap();
        assert_eq!(result.parsed.total_hits, 1);
        assert_eq!(result.parsed.records[0].id, "MED:41234567");
        assert_eq!(
            result.parsed.records[0].title,
            "Reproducible single-cell analysis"
        );
        assert!(result.user_notice.contains("article-level license"));
        assert_eq!(result.artifacts.len(), 1);
        assert_eq!(result.audits.len(), 1);
        assert!(result.evidence[0].source.contains("www.ebi.ac.uk"));
        let before = serde_json::to_value(&result).unwrap();
        drop(store);
        let reopened = ScienceStore::new(temp.path());
        let replay = FetchResult {
            run: reopened.load_run(&result.run.context.run_id).unwrap(),
            artifacts: reopened.artifacts(&result.run.context.run_id).unwrap(),
            evidence: reopened.evidence(&result.run.context.run_id).unwrap(),
            provenance: reopened.provenance(&result.run.context.run_id).unwrap(),
            approvals: reopened.approvals(&result.run.context.run_id).unwrap(),
            audits: result.audits.clone(),
            user_notice: result.user_notice.clone(),
            parsed: parse_responses(
                "europepmc",
                &[exchange(
                    "europepmc",
                    &super::super::europepmc::search_path("single cell RNA", 5),
                    EUROPEPMC,
                )],
            )
            .unwrap(),
            replay_after: result.replay_after,
        };
        assert_eq!(before, serde_json::to_value(replay).unwrap());
    }

    #[test]
    fn malformed_response_fails_run_closed() {
        let temp = tempfile::tempdir().unwrap();
        let store = ScienceStore::new(temp.path());
        let context = csv::fixture_context(temp.path(), ProjectId::new("p"), "alice");
        let run_id = context.run_id.clone();
        let error = execute_approved_fetch(
            &store,
            context,
            "chembl",
            "aspirin",
            vec![exchange(
                "chembl",
                "/molecule/search.json?q=aspirin&limit=5&offset=0",
                b"garbage",
            )],
        )
        .unwrap_err();
        assert!(error.to_string().contains("failed closed"));
        let run = store.load_run(&run_id).unwrap();
        assert_eq!(run.state, RunState::Failed);
        assert!(store.artifacts(&run_id).unwrap().is_empty());
        assert!(store.evidence(&run_id).unwrap().is_empty());
    }

    #[test]
    fn exchange_count_is_enforced_per_protocol() {
        assert_eq!(expected_exchanges("pubmed"), Some(2));
        assert_eq!(expected_exchanges("chembl"), Some(1));
        assert_eq!(expected_exchanges("crossref"), Some(1));
        assert_eq!(expected_exchanges("uniprot"), Some(1));
        assert_eq!(expected_exchanges("unknown"), None);
        assert!(
            parse_responses("pubmed", &pubmed_exchanges()[..1]).is_err(),
            "pubmed without esummary must fail"
        );
        assert!(parse_responses("chembl", &[]).is_err());
        assert!(parse_responses("crossref", &[]).is_err());
        assert!(parse_responses("uniprot", &[]).is_err());
    }
}
