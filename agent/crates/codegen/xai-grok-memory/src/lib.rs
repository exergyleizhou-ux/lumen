//! Memory system for cross-session knowledge persistence.
//!
//! This crate provides a markdown-based memory storage layer that allows
//! Grok to persist important information across sessions. Memory files are
//! stored under `~/.grok/memory/` with workspace-scoped subdirectories
//! keyed by a blake3 hash of the workspace path.
//!
//! ## Data Layout
//!
//! ```text
//! ~/.grok/memory/
//!   ├── MEMORY.md                         # Global curated knowledge
//!   └── {workspace_hash}/                 # Per-workspace (blake3(cwd)[..16])
//!       ├── MEMORY.md                     # Project-level curated knowledge
//!       └── sessions/
//!           └── YYYY-MM-DD-{slug}-{sid8}.md  # Session logs
//! ```
//!
//! ## Feature Flag
//!
//! Memory is gated behind `--experimental-memory` CLI flag or
//! `GROK_MEMORY=1` environment variable. When disabled, this crate
//! is not initialized by the host.

pub mod agent_sandbox;
pub mod capability_grant;
pub mod archive;
pub mod handoff_packet;
pub mod evidence_loop;
pub mod m1_governed_tree_preview;
pub mod bounded_assignment_apply;
pub mod client_advisor_shadow;
pub mod client_advisor_consult;
pub mod harness_regression;
pub mod kairos_supervisor;
pub mod kairos_lease_consumer;
pub mod operator_control;
pub mod nextgen_contract_gates;
pub mod nextgen_exit_gates;
pub mod sealed_attempt_receipt;
pub mod authority_event;
pub mod authority_projection;
pub mod backend;
pub mod canonical;
pub mod chunker;
pub mod claim_authority;
pub mod compose_ng03c;
pub mod context_manifest;
pub mod dream;
pub mod dream_lock;
pub mod effect_recovery;
pub mod embedding;
pub mod governed_assignment;
pub mod governed_operation;
pub mod index;
pub mod lifecycle_journal;
pub mod mmr;
pub mod offline_golden;
pub mod query_expansion;
pub mod schema;
pub mod search;
pub mod storage;
pub mod task_ledger;
pub mod text_utils;
pub mod tool_contract;
pub mod watcher;

pub use agent_sandbox::{
    AgentSandboxState, AgentSandboxV1, FilesystemWriteMode, IssueSandboxRequest, MemoryCapability,
    NetworkMode, SANDBOX_HARD_MAX_DEPTH, SandboxAssuranceV1, SandboxDenyReason,
    AGENT_SANDBOX_SCHEMA_VERSION,
};
pub use capability_grant::{
    CAPABILITY_GRANT_SCHEMA_VERSION, CapabilityGrantProjectionV1, CapabilityGrantState,
    CapabilityGrantV1, GRANT_MIN_TTL_SECS, GrantCapabilityClass, GrantDenyReason, IssueGrantRequest,
};
pub use handoff_packet::{
    HandoffDenyReason, HandoffPacketV1, HANDOFF_MAX_BYTES, HANDOFF_PACKET_SCHEMA_VERSION,
};
pub use evidence_loop::{
    DEFAULT_NO_PROGRESS_CAP, DEFAULT_REPAIR_CAP, LoopEffect, LoopEvent, LoopPhase,
    LoopReduceError, NodeLoopState, SupervisorLoopEvent, SupervisorLoopState, TreeLoopEvent,
    TreeLoopState, reduce_node_loop, reduce_supervisor_loop, reduce_tree_loop,
};
pub use bounded_assignment_apply::{
    AssignmentApplyDeny, AssignmentApplyRequest, AssignmentLifecycle, authorize_assignment_apply,
};
pub use client_advisor_shadow::{
    AdviceReportV1, AdvisorDeny, AdvisorMode, advice_may_mutate_authority, issue_shadow_advice,
};
pub use client_advisor_consult::{
    AdvisorCapsuleDeny, AdvisorContextCapsuleV1, AdvisorRequestKind, AdvisorRequestV1,
    AdvisorUsageReceiptV1, ConsultBlockReason, ConsultOutcome, TokenUsage, build_advisor_capsule,
    build_usage_receipt, consult_timed_out, report_hash,
};
pub use harness_regression::{
    CorpusId, CorpusRunReport, CorpusScenario, HARNESS_CORPUS_SCHEMA_V1, corpus_manifest,
    run_all_corpora,
};
pub use kairos_supervisor::{
    KairosCommand, KairosDeny, KairosSupervisorState, apply_kairos_command,
    note_external_effect_unknown,
};
pub use kairos_lease_consumer::{
    ConsumerOperation, ConsumerPolicy, ConsumerStep, lease_is_expired, outbox_should_deliver,
};
pub use operator_control::{
    OperatorCommand, OperatorDeny, OperatorReceipt, OperationView, ResumeApproval,
    apply_operator_command, issue_resume_approval,
};
pub use nextgen_contract_gates::{GateResult, NextGenContractGateReceipt, run_offline_contract_gates};
pub use nextgen_exit_gates::{
    ADVISOR_CONSULT_TOOL_NAME, AdvisorConsultProjectionV1, AppliedAssignmentChain,
    AppliedChainDeny, ContextRebuildDeny, ContextRebuildRequest, ExpertRepairAdmission,
    ExpertRepairDeny, HandoffDeliveryError, RollbackReceiptV1, authorize_applied_assignment_chain,
    authorize_context_rebuild, authorize_expert_repair_pass, authorize_rollback_receipt,
    deliver_handoff_receipt, invoke_advisor_consult_tool, kairos_fake_clock_lease_cycle,
    operator_control_five_command_matrix,
};
pub use sealed_attempt_receipt::{
    AttemptSealTracker, DURABLE_CLEAN_MAX_IN_PROCESS_RETRIES, DurableSealAuthority, Obs,
    RetryAdmissionRequest, RetryDenyReason, SEALED_RECEIPT_SCHEMA_VERSION,
    SealedAttemptReceiptRecord, SealedAttemptReceiptStore, SealedAttemptReceiptV1,
    SealedReceiptStoreError, authorize_in_process_retry_budget, clean_preflight_receipt,
    effective_retry_budget, mark_attempt_started, mark_external_effect_unknown,
    mark_output_emitted, mark_tool_call, may_in_process_retry, ordinary_turn_max_retries,
    ordinary_turn_max_retries_with_authority,
};
pub use m1_governed_tree_preview::{
    DenyMechanism, DenyRecord, M1PreviewReceipt, TreeNodeProjection, run_m1_governed_tree_preview,
};
pub use backend::{EndpointScopedCredentials, MemoryBackendImpl, MemoryBackendParams};
pub use claim_authority::{
    ClaimAuthority, ClaimAuthorityActor, ClaimDenyReason, ClaimTransitionRequest,
};
pub use context_manifest::{
    ContextManifestError, ContextManifestV1, ManifestAdmissionDenyReason, ManifestAdmissionMode,
    ManifestAdmissionRequest, admit_context_manifest, admit_spawn_receipt,
};
pub use tool_contract::{
    ToolContractV1, ToolDispatchDeny, ToolDispatchSurface, ToolResultEnvelopeV1,
    authorize_tool_dispatch, clamp_tool_result_text, contract_from_runtime_kind,
    force_result_projection,
};
pub use governed_assignment::{
    RootGovernedAssignmentError, RootGovernedAssignmentStore, RootGovernedAssignmentV1,
};
pub use authority_projection::{
    AuthorityProjectionContext, AuthorityProjectionError, project_authority_event,
    project_authority_trail,
};
pub use governed_operation::{
    ExternalEffectState, GovernedOperation, GovernedOperationState, GovernedOperationStore,
    OPS_SNAPSHOT_SCHEMA_VERSION, OperationDenyReason, OutboxDeliveryState, OutboxRecordV1,
    TreeBudgetLedger,
};
pub use index::{MemoryIndex, init_sqlite_vec};
pub use storage::{MemoryScope, MemoryStorage};
pub use task_ledger::{
    AcceptedLedgerSnapshot, WorkingMemoryFact, WorkingMemoryLedger, WorkingMemoryLedgerBackend,
    WorkingMemoryLedgerError, WorkingMemoryLedgerRepair, WorkingMemoryPromotion,
    WorkingMemoryState,
};

/// Embed all chunks that don't have embeddings yet.
///
/// Queries the index for unembedded chunks, batches them through the
/// embedding provider, and upserts the results. Logs progress.
///
/// This is the async glue between the sync `MemoryIndex` and the async
/// `EmbeddingProvider`. Call after reindex, flush writes, or session-end writes.
pub async fn embed_missing_chunks(
    index: &MemoryIndex,
    provider: &dyn embedding::EmbeddingProvider,
) -> usize {
    let chunks = match index.chunks_without_embeddings() {
        Ok(c) if c.is_empty() => return 0,
        Ok(c) => c,
        Err(e) => {
            tracing::warn!(
                target: xai_grok_telemetry::memory_log::TARGET,
                error = %e,
                "failed to query chunks without embeddings"
            );
            return 0;
        }
    };

    let total = chunks.len();
    let mut embedded = 0;

    // Batch in groups of 32 (provider's typical max batch size)
    for batch in chunks.chunks(32) {
        let texts: Vec<&str> = batch.iter().map(|(_, text)| text.as_str()).collect();
        match provider.embed_batch(&texts).await {
            Ok(embeddings) => {
                for ((chunk_id, _), embedding) in batch.iter().zip(embeddings.iter()) {
                    if let Err(e) = index.upsert_embedding(chunk_id, embedding) {
                        tracing::warn!(
                            target: xai_grok_telemetry::memory_log::TARGET,
                            chunk_id,
                            error = %e,
                            "failed to upsert embedding"
                        );
                    } else {
                        embedded += 1;
                    }
                }
            }
            Err(e) => {
                tracing::warn!(
                    target: xai_grok_telemetry::memory_log::TARGET,
                    error = %e,
                    batch_size = texts.len(),
                    "embedding batch failed, skipping"
                );
            }
        }
    }

    if embedded > 0 {
        tracing::info!(
            target: xai_grok_telemetry::memory_log::TARGET,
            embedded,
            total,
            "embedded missing chunks"
        );
    }
    embedded
}
