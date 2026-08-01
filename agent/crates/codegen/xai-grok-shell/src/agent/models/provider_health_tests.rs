use super::*;

#[test]
fn provider_health_tracks_only_passive_connectivity_failures() {
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
}

#[test]
fn provider_failure_domain_includes_non_default_port() {
    assert_eq!(
        provider_failure_domain("https://provider.example.test:8443/v1"),
        Some("provider.example.test:8443".to_owned())
    );
    assert_eq!(provider_failure_domain("not a url"), None);
}
