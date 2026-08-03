//! NG-10A fail-closed wiring: the Lumen product (2.x) update entry points
//! must refuse before RC / without a valid `ReleaseSourceTupleV1`, while the
//! upstream Grok-identity lane (0.x) keeps its existing behaviour.
//!
//! The gate keys on the *installed* version, which honours
//! `GROK_TEST_VERSION` — so these tests pin the product (2.0.0-alpha.1 →
//! refused) and the Grok lane (0.2.5 → still updates) with the same fake
//! channel infrastructure as the existing suites.

#![cfg(unix)]

mod common;

use serial_test::serial;

use common::{
    FakeBinGuard, can_exec_shell_scripts, make_update_config, reset_home, set_test_version,
    test_home,
};
use xai_grok_update::auto_update::{
    auto_update_target, check_update_background, check_update_status, ensure_latest_on_disk,
    run_update, run_update_if_available, UpdateRunMode,
};

/// Fake `gh` that logs argv to `<dir>/gh-args.log` and would answer a
/// `release list` from `<dir>/gh-stable-only-stdout` if ever invoked. Used to
/// prove the pre-RC gate refuses *before* any channel fetch.
fn fake_gh_serving(dir: &std::path::Path) -> String {
    let dq = format!("'{}'", dir.to_string_lossy().replace('\'', "'\\''"));
    format!(
        r#"#!/bin/sh
echo "$@" >> {dq}/gh-args.log
case "$*" in
  *"release list"*)
    if [ -f {dq}/gh-stable-only-stdout ]; then cat {dq}/gh-stable-only-stdout; fi
    ;;
  *"release download"*)
    out=""; prev=""
    for a in "$@"; do
      if [ "$prev" = "--output" ]; then out="$a"; fi
      prev="$a"
    done
    if [ -n "$out" ]; then printf '#!/bin/sh\nexit 0\n' > "$out"; chmod +x "$out"; fi
    ;;
esac
exit 0
"#
    )
}

fn setup_gh(running_version: &str) -> FakeBinGuard {
    let _ = test_home();
    reset_home();
    set_test_version(running_version);
    // SAFETY: serial_test ensures no race; reset_home clears this between tests.
    unsafe { std::env::set_var("GROK_INSTALLER", "gh-release") };
    FakeBinGuard::install("gh", fake_gh_serving)
}

// ─────────────────────────────────────────────────────────────────────────────
// Pre-RC Lumen product (2.0.0-alpha.1): every entry point fails closed.
// ─────────────────────────────────────────────────────────────────────────────

#[tokio::test]
#[serial]
async fn pre_rc_lumen_run_update_fails_closed() {
    let g = setup_gh("2.0.0-alpha.1");
    g.set_stable_only_stdout("v0.2.7\n");
    let mut cfg = make_update_config("stable");
    let err = run_update(false, None, None, &mut cfg)
        .await
        .expect_err("pre-RC Lumen update must refuse");
    assert!(
        err.to_string().contains("release gate"),
        "must explain the fail-closed gate: {err}"
    );
    assert!(
        err.to_string().contains("pre-RC"),
        "must name the pre-RC state: {err}"
    );
}

#[tokio::test]
#[serial]
async fn pre_rc_lumen_run_update_never_touches_the_network() {
    let g = setup_gh("2.0.0-alpha.1");
    g.set_stable_only_stdout("v0.2.7\n");
    let mut cfg = make_update_config("stable");
    let _ = run_update(false, None, None, &mut cfg).await;
    assert_eq!(
        g.args_log().len(),
        0,
        "pre-RC gate must refuse before any gh/npm invocation"
    );
}

#[tokio::test]
#[serial]
async fn pre_rc_lumen_check_status_refuses_without_fetching() {
    let g = setup_gh("2.0.0-alpha.1");
    g.set_stable_only_stdout("v0.2.7\n");
    let cfg = make_update_config("stable");
    let status = check_update_status(&cfg).await;
    assert!(!status.update_available, "pre-RC must not advertise an update");
    assert!(status.latest_version.is_none());
    let error = status
        .error
        .as_deref()
        .expect("fail-closed status carries an error");
    assert!(error.contains("release gate"), "error must explain the gate: {error}");
    assert_eq!(g.args_log().len(), 0, "no fetch before the gate refusal");
}

#[tokio::test]
#[serial]
async fn pre_rc_lumen_auto_update_target_is_none() {
    let g = setup_gh("2.0.0-alpha.1");
    g.set_stable_only_stdout("v0.2.7\n");
    let cfg = make_update_config("stable");
    assert_eq!(auto_update_target(&cfg).await, None);
    assert_eq!(g.args_log().len(), 0);
}

#[tokio::test]
#[serial]
async fn pre_rc_lumen_ensure_latest_on_disk_is_a_noop() {
    let g = setup_gh("2.0.0-alpha.1");
    g.set_stable_only_stdout("v0.2.7\n");
    let cfg = make_update_config("stable");
    let outcome = ensure_latest_on_disk(&cfg).await.unwrap();
    assert_eq!(
        outcome.installed, None,
        "pre-RC leader pass must not install anything"
    );
    assert!(!outcome.relaunch_needed);
    assert_eq!(g.args_log().len(), 0);
}

#[tokio::test]
#[serial]
async fn pre_rc_lumen_background_check_is_none() {
    let g = setup_gh("2.0.0-alpha.1");
    g.set_stable_only_stdout("v0.2.7\n");
    let cfg = make_update_config("stable");
    let check = check_update_background(&cfg).await;
    assert!(
        check.update.is_none(),
        "pre-RC must not offer a background update"
    );
    assert!(check.download.is_none());
    assert_eq!(g.args_log().len(), 0);
}

#[tokio::test]
#[serial]
async fn pre_rc_lumen_run_update_if_available_is_a_noop() {
    let g = setup_gh("2.0.0-alpha.1");
    g.set_stable_only_stdout("v0.2.7\n");
    let cfg = make_update_config("stable");
    let ran = run_update_if_available(UpdateRunMode::Blocking, false, &cfg)
        .await
        .unwrap();
    assert!(!ran, "pre-RC auto-update must be a silent no-op");
    assert_eq!(g.args_log().len(), 0);
}

// ─────────────────────────────────────────────────────────────────────────────
// Grok-identity lane (0.x): the gate leaves existing behaviour untouched.
// ─────────────────────────────────────────────────────────────────────────────

#[tokio::test]
#[serial]
async fn grok_identity_lane_still_updates() {
    if !can_exec_shell_scripts() {
        eprintln!("skipping: shell scripts cannot execute in this sandbox");
        return;
    }
    let g = setup_gh("0.2.5");
    g.set_stable_only_stdout("v0.2.7\n");
    let mut cfg = make_update_config("stable");
    let installed = run_update(false, None, None, &mut cfg).await.unwrap();
    assert_eq!(
        installed.as_deref(),
        Some("0.2.7"),
        "Grok-identity lane must keep updating (0.x is outside Lumen authority)"
    );
}
