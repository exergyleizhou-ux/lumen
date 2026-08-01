use super::*;

#[test]
fn provider_health_tracks_only_passive_routing_failures() {
    let manager = ModelsManager::default();
    let endpoint = "https://api.example.test/v1/chat/completions";
    assert_eq!(
        manager.provider_health(endpoint),
        ProviderHealthSnapshot::Unknown
    );

    manager.record_provider_failure(endpoint, "auth");
    manager.record_provider_failure(endpoint, "configuration");
    assert_eq!(
        manager.provider_health(endpoint),
        ProviderHealthSnapshot::Unknown,
        "auth/config failures are model or credential problems, not provider health"
    );

    manager.record_provider_failure(endpoint, "rate_limited");
    assert_eq!(
        manager.provider_health("https://API.EXAMPLE.TEST/v1/other-route"),
        ProviderHealthSnapshot::Degraded {
            failure_kind: "rate_limited".to_owned()
        },
        "one endpoint domain must share passive health across route suffixes"
    );

    manager.record_provider_failure(endpoint, "quota_exhausted");
    assert_eq!(
        manager.provider_health(endpoint),
        ProviderHealthSnapshot::Degraded {
            failure_kind: "quota_exhausted".to_owned()
        },
        "a passive credit-limit response may skip a provider for a later task"
    );
}

#[test]
fn explicit_pool_selects_healthy_priority_without_expanding_allowlist() {
    let manager = ModelsManager::default();
    let entry = |model: &str, base_url: &str| {
        let mut info = crate::agent::config::ModelInfo::fallback(model);
        info.base_url = base_url.to_owned();
        crate::agent::config::ModelEntry {
            info,
            api_key: Some("test-key".to_owned()),
            env_key: None,
            auth_provider: None,
            api_base_url: Some(base_url.to_owned()),
        }
    };
    manager.insert_test_entry("flash", entry("flash", "https://flash.example.test/v1"));
    manager.insert_test_entry("grok", entry("grok", "https://grok.example.test/v1"));
    manager.record_provider_failure("https://flash.example.test/v1", "quota_exhausted");

    assert_eq!(
        manager.select_healthy_model_from_pool(
            &["flash".to_owned(), "grok".to_owned()],
            &["flash".to_owned(), "outside".to_owned(), "grok".to_owned()],
            "flash",
        ),
        Some("grok".to_owned())
    );
    assert_eq!(
        manager.select_healthy_model_from_pool(&["flash".to_owned()], &[], "flash"),
        None,
        "the pool is an allowlist, not a request to use another catalog model"
    );
}

#[test]
fn provider_failure_domain_includes_non_default_port() {
    assert_eq!(
        provider_failure_domain("https://provider.example.test:8443/v1"),
        Some("provider.example.test:8443".to_owned())
    );
    assert_eq!(provider_failure_domain("not a url"), None);
}
