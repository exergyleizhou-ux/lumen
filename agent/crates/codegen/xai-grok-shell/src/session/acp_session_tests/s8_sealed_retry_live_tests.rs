//! S8 Critic M1/M2: live P4b side conditions + auth-class decision path.
//!
//! Proves pool_exhausted / breaker_open / stale_advice are derived from
//! SessionActor live state (models_manager + advice epoch atomics), not
//! hardcoded false. Full SamplerActor resubmit loop is covered by the pure
//! `decide_auth_class_retry` matrix in `nextgen_control` (mocking the real
//! actor handle's completion oneshot is not practical; the production loop
//! body only composes that table + live flags).

use super::support::*;
use super::*;
use crate::session::nextgen_control::{
    AuthClassRetryAction, authorize_ordinary_retry_budget, decide_auth_class_retry,
    ordinary_retry_admission,
};
use std::sync::atomic::Ordering;
use xai_grok_memory::{
    DurableSealAuthority, RetryDenyReason, SealedAttemptReceiptStore, clean_preflight_receipt,
};

fn test_model_entry(model: &str, base_url: &str) -> crate::agent::config::ModelEntry {
    let mut info = crate::agent::config::ModelInfo::fallback(model);
    info.base_url = base_url.to_owned();
    crate::agent::config::ModelEntry {
        info,
        api_key: Some("test-key".to_owned()),
        env_key: None,
        auth_provider: None,
        api_base_url: Some(base_url.to_owned()),
    }
}

#[tokio::test(flavor = "multi_thread")]
async fn live_pool_exhausted_denies_auth_class_admission() {
    let local = tokio::task::LocalSet::new();
    local
        .run_until(async {
            let (gateway_tx, _gateway_rx) = tokio::sync::mpsc::unbounded_channel();
            let (persistence_tx, _persistence_rx) = tokio::sync::mpsc::unbounded_channel();
            let actor = std::sync::Arc::new(
                create_test_actor(0, 32_000, 85, gateway_tx, persistence_tx).await,
            );
            actor.models_manager.insert_test_entry(
                "m1",
                test_model_entry("m1", "https://m1.example.test/v1"),
            );
            actor.models_manager.insert_test_entry(
                "m2",
                test_model_entry("m2", "https://m2.example.test/v1"),
            );
            // Degrade both pool endpoints → select_healthy returns None.
            actor
                .models_manager
                .record_provider_failure("https://m1.example.test/v1", "upstream");
            actor
                .models_manager
                .record_provider_failure("https://m2.example.test/v1", "upstream");
            actor.models_manager.set_model_routing_config(
                crate::agent::config::ModelRoutingConfig {
                    enabled: true,
                    model_pool: vec!["m1".into(), "m2".into()],
                    priority: vec![],
                    task_preferences: Default::default(),
                },
            );

            let side = actor.collect_p4b_live_side_conditions("https://m1.example.test/v1");
            assert!(
                side.pool_exhausted,
                "live pool with all degraded endpoints must be exhausted"
            );

            let clean = clean_preflight_receipt("live-pool");
            let store = SealedAttemptReceiptStore::in_memory();
            store.record(clean.clone(), None, None).unwrap();
            let authority = store.authority_for(&clean);
            assert_eq!(authority, DurableSealAuthority::ConfirmedClean);

            let err = authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&clean),
                authority,
                false,
                side.pool_exhausted,
                side.breaker_open,
                side.stale_advice,
                1,
                0,
            ))
            .unwrap_err();
            assert_eq!(err, RetryDenyReason::PoolExhausted);
            assert_eq!(
                decide_auth_class_retry(true, false, Err(err), true, 0),
                AuthClassRetryAction::Terminal {
                    reason: "retry.pool_exhausted"
                }
            );
        })
        .await;
}

#[tokio::test(flavor = "multi_thread")]
async fn live_breaker_open_denies_auth_class_admission() {
    let local = tokio::task::LocalSet::new();
    local
        .run_until(async {
            let (gateway_tx, _gateway_rx) = tokio::sync::mpsc::unbounded_channel();
            let (persistence_tx, _persistence_rx) = tokio::sync::mpsc::unbounded_channel();
            let actor = std::sync::Arc::new(
                create_test_actor(0, 32_000, 85, gateway_tx, persistence_tx).await,
            );
            let base = "https://broken.example.test/v1";
            actor
                .models_manager
                .insert_test_entry("broken", test_model_entry("broken", base));
            actor
                .models_manager
                .record_provider_failure(base, "timeout");

            let side = actor.collect_p4b_live_side_conditions(base);
            assert!(
                side.breaker_open,
                "Degraded provider_health must surface as breaker_open"
            );

            let clean = clean_preflight_receipt("live-breaker");
            let err = authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&clean),
                DurableSealAuthority::ConfirmedClean,
                false,
                side.pool_exhausted,
                side.breaker_open,
                side.stale_advice,
                1,
                0,
            ))
            .unwrap_err();
            assert_eq!(err, RetryDenyReason::BreakerOpen);
            assert_eq!(
                decide_auth_class_retry(true, false, Err(err), true, 0),
                AuthClassRetryAction::Terminal {
                    reason: "retry.breaker_open"
                }
            );
        })
        .await;
}

#[tokio::test(flavor = "multi_thread")]
async fn live_stale_advice_denies_auth_class_admission() {
    let local = tokio::task::LocalSet::new();
    local
        .run_until(async {
            let (gateway_tx, _gateway_rx) = tokio::sync::mpsc::unbounded_channel();
            let (persistence_tx, _persistence_rx) = tokio::sync::mpsc::unbounded_channel();
            let actor = std::sync::Arc::new(
                create_test_actor(0, 32_000, 85, gateway_tx, persistence_tx).await,
            );

            // Simulate advice issued at epoch 1, then policy advanced to 2.
            let live = actor.tool_context.live_policy_epoch.load(Ordering::Relaxed);
            assert_eq!(live, 1);
            actor
                .tool_context
                .advice_issued_policy_epoch
                .store(1, Ordering::Relaxed);
            actor
                .tool_context
                .live_policy_epoch
                .store(2, Ordering::Relaxed);

            let side = actor.collect_p4b_live_side_conditions("https://any.example.test/v1");
            assert!(side.stale_advice, "epoch lag must be stale_advice");

            let clean = clean_preflight_receipt("live-stale");
            let err = authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&clean),
                DurableSealAuthority::ConfirmedClean,
                false,
                side.pool_exhausted,
                side.breaker_open,
                side.stale_advice,
                1,
                0,
            ))
            .unwrap_err();
            assert_eq!(err, RetryDenyReason::StaleAdvice);
            assert_eq!(
                decide_auth_class_retry(true, false, Err(err), true, 0),
                AuthClassRetryAction::Terminal {
                    reason: "retry.stale_advice"
                }
            );
        })
        .await;
}

#[tokio::test(flavor = "multi_thread")]
async fn live_healthy_side_conditions_allow_clean_seal_admission() {
    let local = tokio::task::LocalSet::new();
    local
        .run_until(async {
            let (gateway_tx, _gateway_rx) = tokio::sync::mpsc::unbounded_channel();
            let (persistence_tx, _persistence_rx) = tokio::sync::mpsc::unbounded_channel();
            let actor = std::sync::Arc::new(
                create_test_actor(0, 32_000, 85, gateway_tx, persistence_tx).await,
            );
            // No routing pool, no degradation, no advice → all clear.
            let side = actor.collect_p4b_live_side_conditions("https://ok.example.test/v1");
            assert!(!side.pool_exhausted);
            assert!(!side.breaker_open);
            assert!(!side.stale_advice);

            let clean = clean_preflight_receipt("live-ok");
            let remaining = authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&clean),
                DurableSealAuthority::ConfirmedClean,
                false,
                side.pool_exhausted,
                side.breaker_open,
                side.stale_advice,
                1,
                0,
            ))
            .expect("healthy live flags + clean seal must admit");
            assert_eq!(remaining, 1);
            assert_eq!(
                decide_auth_class_retry(true, false, Ok(remaining), true, 0),
                AuthClassRetryAction::Resubmit { next_used: 1 }
            );
        })
        .await;
}

#[test]
fn seal_observations_tool_call_phase_marks_tool_emitted() {
    // Real attempt-scoped path (matches seal_observations_from_streaming_capture).
    use crate::session::acp_session::CapturePhase;
    use crate::session::streaming_capture::StreamingTurnCapture;

    let mut cap = StreamingTurnCapture::default();
    cap.phase = CapturePhase::ToolCall;
    let (had_output, had_tool) = cap.attempt_seal_observations();
    assert!(!had_output);
    assert!(had_tool);

    let mut tracker = crate::session::nextgen_control::begin_attempt_seal("phase-tool");
    tracker.apply_failure_observations(false, had_tool, true);
    assert_eq!(
        tracker.may_retry().unwrap_err(),
        RetryDenyReason::ToolCallEmitted
    );
}

#[test]
fn pre_stream_transport_fail_seals_clean_despite_prior_segments() {
    // Regression: cli-chat-proxy "error sending request" before StreamStarted
    // used to inherit prior-generation segments / token stamps and refuse
    // the S8 clean-seal transport resubmit.
    use crate::session::acp_session::CapturePhase;
    use crate::session::streaming_capture::StreamingTurnCapture;

    let mut cap = StreamingTurnCapture::default();
    cap.begin_turn(Some("goal-1".into()), 3);
    cap.start_stream(1);
    cap.append(false, "earlier tool-loop assistant text");
    cap.phase = CapturePhase::ToolCall;
    cap.start_stream(2); // folds prior gen into segments; current slot empty
    cap.reasoning_tokens = Some(2048);
    cap.completion_tokens = Some(64);

    // New attempt slot (as run_turn_via_sampler does before submit).
    cap.prepare_attempt_slot();
    let (had_output, had_tool) = cap.attempt_seal_observations();
    assert!(!had_output && !had_tool);

    let mut tracker = crate::session::nextgen_control::begin_attempt_seal("pre-stream");
    tracker.apply_failure_observations(had_output, had_tool, true);
    assert!(
        tracker.may_retry().is_ok(),
        "clean pre-stream transport fail must admit in-process retry"
    );

    // End-to-end with durable store: ConfirmedClean + transport candidate.
    let store = xai_grok_memory::SealedAttemptReceiptStore::in_memory();
    let (receipt, authority) = crate::session::nextgen_control::seal_and_authority(
        &store,
        tracker.receipt().clone(),
        None,
        None,
    );
    assert_eq!(authority, DurableSealAuthority::ConfirmedClean);
    let admit = authorize_ordinary_retry_budget(&ordinary_retry_admission(
        Some(&receipt),
        authority,
        false,
        false,
        false,
        false,
        1,
        0,
    ));
    assert_eq!(
        decide_auth_class_retry(
            false,
            true, // Http / EventStreamError transport candidate
            admit,
            false,
            0,
        ),
        AuthClassRetryAction::Resubmit { next_used: 1 }
    );
}
