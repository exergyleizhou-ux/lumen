use super::support::*;
use super::*;

fn sampling_error(
    kind: xai_grok_sampler::SamplingErrorKind,
    status_code: Option<u16>,
    message: &str,
) -> xai_grok_sampler::SamplingErrorInfo {
    xai_grok_sampler::SamplingErrorInfo {
        kind,
        status_code,
        message: message.to_owned(),
        is_retryable: false,
        retry_after_secs: None,
        model_metadata: None,
        empty_response_context: None,
        doom_loop_triggers: None,
        doom_loop_aborted_at_chunk: None,
    }
}

#[test]
fn quota_responses_mark_only_later_task_routing_as_degraded() {
    use xai_grok_sampler::SamplingErrorKind;

    assert_eq!(
        provider_failure_kind_for_sampling_error(&sampling_error(
            SamplingErrorKind::Api,
            Some(402),
            "payment required"
        )),
        Some("quota_exhausted")
    );
    assert_eq!(
        provider_failure_kind_for_sampling_error(&sampling_error(
            SamplingErrorKind::Api,
            Some(400),
            "Spending limit has been reached"
        )),
        Some("quota_exhausted")
    );
    assert_eq!(
        provider_failure_kind_for_sampling_error(&sampling_error(
            SamplingErrorKind::Auth,
            Some(401),
            "invalid key"
        )),
        None,
        "a credential error must not masquerade as no quota"
    );
}

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
async fn expert_pool_skips_quota_exhausted_priority_before_a_new_task() {
    let local = tokio::task::LocalSet::new();
    local
        .run_until(async {
            let (gateway_tx, _gateway_rx) = tokio::sync::mpsc::unbounded_channel();
            let (persistence_tx, _persistence_rx) = tokio::sync::mpsc::unbounded_channel();
            let actor = std::sync::Arc::new(
                create_test_actor(0, 32_000, 85, gateway_tx, persistence_tx).await,
            );
            actor.models_manager.insert_test_entry(
                crate::session::expert::FLASH_EXECUTOR_MODEL,
                test_model_entry(
                    crate::session::expert::FLASH_EXECUTOR_MODEL,
                    "https://flash.example.test/v1",
                ),
            );
            actor.models_manager.insert_test_entry(
                crate::session::expert::GROK_MODEL,
                test_model_entry(
                    crate::session::expert::GROK_MODEL,
                    "https://grok.example.test/v1",
                ),
            );
            actor.models_manager.record_provider_failure(
                "https://grok.example.test/v1/chat/completions",
                "quota_exhausted",
            );
            let mut expert = crate::session::expert::ExpertModeState::configured();
            expert.require_consult_on_medium = false;
            expert.advisor_model_pool = vec![
                crate::session::expert::GROK_MODEL.to_owned(),
                crate::session::expert::FLASH_EXECUTOR_MODEL.to_owned(),
            ];
            expert.advisor_model_priority = expert.advisor_model_pool.clone();
            actor.state.lock().await.expert = expert;

            let (guard, _) = actor
                .begin_expert_turn(
                    "implement a deterministic routing rule",
                    crate::session::expert::ExpertMode::Fast,
                    Vec::new(),
                )
                .await
                .expect("a healthy configured fallback must start a new task");
            let expert = actor.state.lock().await.expert.clone();
            assert_eq!(
                expert.executor_requested,
                crate::session::expert::FLASH_EXECUTOR_MODEL
            );
            let evidence = expert.advisor_pool_routing_evidence.last().unwrap();
            assert_eq!(
                evidence.to_model,
                crate::session::expert::FLASH_EXECUTOR_MODEL
            );
            assert_eq!(
                evidence.skipped_unavailable_candidates,
                vec![crate::session::expert::GROK_MODEL.to_owned()]
            );
            assert!(!evidence.had_output);
            actor
                .finish_expert_turn(
                    guard,
                    &Ok(TurnOutcome::Cancelled {
                        category: None,
                        context: None,
                    }),
                )
                .await;
        })
        .await;
}

#[tokio::test(flavor = "multi_thread")]
async fn ordinary_turn_reroutes_only_after_zero_output_provider_failure() {
    let local = tokio::task::LocalSet::new();
    local
        .run_until(async {
            let (gateway_tx, _gateway_rx) = tokio::sync::mpsc::unbounded_channel();
            let (persistence_tx, _persistence_rx) = tokio::sync::mpsc::unbounded_channel();
            let actor = std::sync::Arc::new(
                create_test_actor(0, 32_000, 85, gateway_tx, persistence_tx).await,
            );
            actor.models_manager.insert_test_entry(
                "flash",
                test_model_entry("flash", "https://flash.example.test/v1"),
            );
            actor.models_manager.insert_test_entry(
                "grok",
                test_model_entry("grok", "https://grok.example.test/v1"),
            );
            actor.models_manager.set_model_routing_config(
                crate::agent::config::ModelRoutingConfig {
                    enabled: true,
                    model_pool: vec!["flash".to_owned(), "grok".to_owned()],
                    priority: vec!["flash".to_owned(), "grok".to_owned()],
                },
            );
            let initial = actor
                .resolve_aux_sampler_config("flash")
                .await
                .expect("test flash catalog entry resolves");
            actor
                .handle_set_session_model(initial, false, false, true, 85)
                .await
                .expect("test session accepts flash");

            assert!(matches!(
                actor
                    .handle_sampling_failure(sampling_error(
                        xai_grok_sampler::SamplingErrorKind::Api,
                        Some(402),
                        "out of credits",
                    ))
                    .await,
                Ok(SamplerFailureRecovery::RerouteAndResubmit)
            ));
            assert_eq!(
                actor
                    .chat_state_handle
                    .get_sampling_config()
                    .await
                    .expect("sampling config")
                    .model,
                "grok"
            );
            assert_eq!(actor.models_manager.current_model_id().0.as_ref(), "grok");
            assert!(
                !actor.models_manager.user_selected_model(),
                "automatic reroute must not turn into a user model pin"
            );
        })
        .await;
}

#[tokio::test(flavor = "multi_thread")]
async fn explicit_model_pin_blocks_ordinary_turn_reroute() {
    let local = tokio::task::LocalSet::new();
    local
        .run_until(async {
            let (gateway_tx, _gateway_rx) = tokio::sync::mpsc::unbounded_channel();
            let (persistence_tx, _persistence_rx) = tokio::sync::mpsc::unbounded_channel();
            let actor = std::sync::Arc::new(
                create_test_actor(0, 32_000, 85, gateway_tx, persistence_tx).await,
            );
            actor.models_manager.insert_test_entry(
                "flash",
                test_model_entry("flash", "https://flash.example.test/v1"),
            );
            actor.models_manager.insert_test_entry(
                "grok",
                test_model_entry("grok", "https://grok.example.test/v1"),
            );
            actor.models_manager.set_model_routing_config(
                crate::agent::config::ModelRoutingConfig {
                    enabled: true,
                    model_pool: vec!["flash".to_owned(), "grok".to_owned()],
                    priority: vec!["grok".to_owned()],
                },
            );
            let initial = actor
                .resolve_aux_sampler_config("flash")
                .await
                .expect("test flash catalog entry resolves");
            actor
                .handle_set_session_model(initial, false, false, true, 85)
                .await
                .expect("test session accepts flash");
            actor
                .models_manager
                .set_current_model_id(acp::ModelId::new("flash"));

            assert!(
                actor
                    .handle_sampling_failure(sampling_error(
                        xai_grok_sampler::SamplingErrorKind::Api,
                        Some(402),
                        "out of credits",
                    ))
                    .await
                    .is_err()
            );
            assert_eq!(
                actor
                    .chat_state_handle
                    .get_sampling_config()
                    .await
                    .expect("sampling config")
                    .model,
                "flash"
            );
        })
        .await;
}

#[tokio::test(flavor = "multi_thread")]
async fn expert_pool_fails_closed_when_no_selected_candidate_is_routable() {
    let local = tokio::task::LocalSet::new();
    local
        .run_until(async {
            let (gateway_tx, _gateway_rx) = tokio::sync::mpsc::unbounded_channel();
            let (persistence_tx, _persistence_rx) = tokio::sync::mpsc::unbounded_channel();
            let actor = create_test_actor(0, 32_000, 85, gateway_tx, persistence_tx).await;
            actor.models_manager.insert_test_entry(
                crate::session::expert::GROK_MODEL,
                test_model_entry(
                    crate::session::expert::GROK_MODEL,
                    "https://grok.example.test/v1",
                ),
            );
            actor.models_manager.record_provider_failure(
                "https://grok.example.test/v1/chat/completions",
                "quota_exhausted",
            );
            let mut expert = crate::session::expert::ExpertModeState::configured();
            expert.require_consult_on_medium = false;
            expert.advisor_model_pool = vec![crate::session::expert::GROK_MODEL.to_owned()];
            expert.advisor_model_priority = expert.advisor_model_pool.clone();
            actor.state.lock().await.expert = expert;

            assert!(matches!(
                actor
                    .begin_expert_turn(
                        "implement a deterministic routing rule",
                        crate::session::expert::ExpertMode::Fast,
                        Vec::new(),
                    )
                    .await,
                Err(crate::session::expert::ExpertErrorCode::ModelMissing)
            ));
            assert!(
                !actor.state.lock().await.expert.is_active(),
                "no candidate must fail before starting an Expert task"
            );
        })
        .await;
}

#[tokio::test(flavor = "multi_thread")]
async fn explicit_user_model_pin_blocks_expert_pool_override() {
    let local = tokio::task::LocalSet::new();
    local
        .run_until(async {
            let (gateway_tx, _gateway_rx) = tokio::sync::mpsc::unbounded_channel();
            let (persistence_tx, _persistence_rx) = tokio::sync::mpsc::unbounded_channel();
            let actor = create_test_actor(0, 32_000, 85, gateway_tx, persistence_tx).await;
            actor.models_manager.insert_test_entry(
                crate::session::expert::PRO_EXECUTOR_MODEL,
                test_model_entry(
                    crate::session::expert::PRO_EXECUTOR_MODEL,
                    "https://pro.example.test/v1",
                ),
            );
            actor.models_manager.insert_test_entry(
                crate::session::expert::FLASH_EXECUTOR_MODEL,
                test_model_entry(
                    crate::session::expert::FLASH_EXECUTOR_MODEL,
                    "https://flash.example.test/v1",
                ),
            );
            actor.models_manager.set_current_model_id(acp::ModelId::new(
                crate::session::expert::PRO_EXECUTOR_MODEL,
            ));
            let mut expert = crate::session::expert::ExpertModeState::configured();
            expert.require_consult_on_medium = false;
            expert.executor_requested = crate::session::expert::PRO_EXECUTOR_MODEL.to_owned();
            expert.advisor_model_pool =
                vec![crate::session::expert::FLASH_EXECUTOR_MODEL.to_owned()];
            expert.advisor_model_priority = expert.advisor_model_pool.clone();
            actor.state.lock().await.expert = expert;

            let (guard, _) = actor
                .begin_expert_turn(
                    "implement a deterministic routing rule",
                    crate::session::expert::ExpertMode::Fast,
                    Vec::new(),
                )
                .await
                .expect("the explicitly selected model remains usable");
            let expert = actor.state.lock().await.expert.clone();
            assert_eq!(
                expert.executor_requested,
                crate::session::expert::PRO_EXECUTOR_MODEL
            );
            assert!(expert.advisor_pool_routing_evidence.is_empty());
            actor
                .finish_expert_turn(
                    guard,
                    &Ok(TurnOutcome::Cancelled {
                        category: None,
                        context: None,
                    }),
                )
                .await;
        })
        .await;
}

#[tokio::test(flavor = "multi_thread")]
async fn explicit_session_pool_is_newer_than_an_older_single_model_pin() {
    let local = tokio::task::LocalSet::new();
    local
        .run_until(async {
            let (gateway_tx, _gateway_rx) = tokio::sync::mpsc::unbounded_channel();
            let (persistence_tx, _persistence_rx) = tokio::sync::mpsc::unbounded_channel();
            let actor = create_test_actor(0, 32_000, 85, gateway_tx, persistence_tx).await;
            actor.models_manager.insert_test_entry(
                crate::session::expert::PRO_EXECUTOR_MODEL,
                test_model_entry(
                    crate::session::expert::PRO_EXECUTOR_MODEL,
                    "https://pro.example.test/v1",
                ),
            );
            actor.models_manager.insert_test_entry(
                crate::session::expert::FLASH_EXECUTOR_MODEL,
                test_model_entry(
                    crate::session::expert::FLASH_EXECUTOR_MODEL,
                    "https://flash.example.test/v1",
                ),
            );
            actor.models_manager.set_current_model_id(acp::ModelId::new(
                crate::session::expert::PRO_EXECUTOR_MODEL,
            ));
            let mut expert = crate::session::expert::ExpertModeState::configured();
            expert.require_consult_on_medium = false;
            expert.executor_requested = crate::session::expert::PRO_EXECUTOR_MODEL.to_owned();
            expert.advisor_model_pool =
                vec![crate::session::expert::FLASH_EXECUTOR_MODEL.to_owned()];
            expert.advisor_model_priority = expert.advisor_model_pool.clone();
            expert.advisor_model_pool_user_override = true;
            actor.state.lock().await.expert = expert;

            let (guard, _) = actor
                .begin_expert_turn(
                    "implement a deterministic routing rule",
                    crate::session::expert::ExpertMode::Fast,
                    Vec::new(),
                )
                .await
                .expect("a newer explicit pool must select its candidate");
            let expert = actor.state.lock().await.expert.clone();
            assert_eq!(
                expert.executor_requested,
                crate::session::expert::FLASH_EXECUTOR_MODEL
            );
            assert!(expert.advisor_model_pool_user_override);
            actor
                .finish_expert_turn(
                    guard,
                    &Ok(TurnOutcome::Cancelled {
                        category: None,
                        context: None,
                    }),
                )
                .await;
        })
        .await;
}
