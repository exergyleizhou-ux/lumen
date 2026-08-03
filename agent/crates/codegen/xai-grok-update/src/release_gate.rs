//! NG-10A fail-closed release gate: the Lumen product must not accept
//! updates before its first release candidate, and never without a valid
//! [`ReleaseSourceTupleV1`].
//!
//! The decision keys on the *installed* product version (honouring
//! `GROK_TEST_VERSION` in tests; production falls back to the compiled-in
//! `xai_grok_version::VERSION`). The Lumen product line is 2.x; upstream
//! Grok-identity lanes (0.x) are outside Lumen's release authority and keep
//! their existing behaviour. For the Lumen line the gate is fail-closed:
//!
//! - pre-RC prerelease (`2.0.0-alpha.1`, `beta`, …)     → `Refused(PreRc)`
//! - RC or formal release without a tuple                → `Refused(MissingReleaseTuple)`
//! - tuple present but structurally invalid or version/lock-mismatched
//!                                                        → `Refused(InvalidReleaseTuple)`
//! - RC or formal release with a valid tuple              → `Allowed`
//!
//! An unparseable installed version fails closed. The wiring helpers
//! (`require_lumen_update_authority`) are called at every product update
//! entry point; until a release tuple source exists in-tree, the Lumen line
//! therefore refuses to check for, advertise, download, or install updates.

use crate::release_tuple::ReleaseSourceTupleV1;
use semver::{Prerelease, Version};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ReleaseGateDecision {
    Allowed,
    Refused(ReleaseGateReason),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ReleaseGateReason {
    /// The installed product is a pre-RC prerelease (e.g. `2.0.0-alpha.1`):
    /// no RC tag exists yet, so no Lumen release tuple can exist either.
    PreRc { version: String },
    /// The product is at RC/formal but no `ReleaseSourceTupleV1` was supplied.
    MissingReleaseTuple { version: String },
    /// A tuple was supplied but failed structural/version/lock validation.
    InvalidReleaseTuple {
        version: String,
        detail: String,
    },
    /// The installed version cannot be parsed; release status is unprovable.
    UnverifiableVersion { version: String },
}

impl std::fmt::Display for ReleaseGateReason {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.message())
    }
}

impl ReleaseGateReason {
    /// User-facing explanation of the fail-closed refusal.
    pub fn message(&self) -> String {
        match self {
            ReleaseGateReason::PreRc { version } => format!(
                "Lumen {version} is pre-RC; the updater is fail-closed until \
                 v2.0.0-rc.1 and a valid ReleaseSourceTupleV1 exist"
            ),
            ReleaseGateReason::MissingReleaseTuple { version } => format!(
                "Lumen {version} has no valid ReleaseSourceTupleV1; refusing to \
                 accept updates without source/evidence provenance"
            ),
            ReleaseGateReason::InvalidReleaseTuple { version, detail } => format!(
                "Lumen {version} has an invalid ReleaseSourceTupleV1: {detail}"
            ),
            ReleaseGateReason::UnverifiableVersion { version } => format!(
                "cannot verify release status of installed version {version:?}; \
                 failing closed"
            ),
        }
    }
}

/// True when the installed version lies on the Lumen product line
/// (major >= 2), the line this release authority owns.
pub fn is_lumen_product_version(version: &str) -> bool {
    matches!(Version::parse(version), Ok(v) if v.major >= 2)
}

/// True when the prerelease identifiers begin with an `rc` marker
/// (`rc`, `rc.1`, …). Anything else (`alpha.*`, `beta.*`, `dev*`) is pre-RC.
fn is_rc_prerelease(pre: &Prerelease) -> bool {
    pre.as_str()
        .split('.')
        .next()
        .is_some_and(|identifier| identifier.starts_with("rc"))
}

/// The pure fail-closed gate. `installed_version` is the running product
/// version; `tuple` is the release tuple the update candidate carries (None
/// before RC); `source_lock_json` optionally supplies the on-disk
/// `SOURCE_LOCK.json` bytes so the tuple's lock digest is verified too.
pub fn release_gate_decision(
    installed_version: &str,
    tuple: Option<&ReleaseSourceTupleV1>,
    source_lock_json: Option<&[u8]>,
) -> ReleaseGateDecision {
    let Ok(version) = Version::parse(installed_version) else {
        return ReleaseGateDecision::Refused(ReleaseGateReason::UnverifiableVersion {
            version: installed_version.to_string(),
        });
    };
    // Upstream Grok-identity lane: outside the Lumen release authority.
    if version.major < 2 {
        return ReleaseGateDecision::Allowed;
    }
    let installed_string = version.to_string();
    // Pre-RC: no RC tag has ever been produced, so no valid tuple can exist.
    if !version.pre.is_empty() && !is_rc_prerelease(&version.pre) {
        return ReleaseGateDecision::Refused(ReleaseGateReason::PreRc {
            version: installed_string,
        });
    }
    // RC or formal release: a valid tuple is mandatory.
    let Some(tuple) = tuple else {
        return ReleaseGateDecision::Refused(ReleaseGateReason::MissingReleaseTuple {
            version: installed_string,
        });
    };
    if let Err(err) = tuple.validate() {
        return ReleaseGateDecision::Refused(ReleaseGateReason::InvalidReleaseTuple {
            version: installed_string,
            detail: err.to_string(),
        });
    }
    let tuple_ok = tuple
        .lumen_version
        .parse::<Version>()
        .is_ok_and(|v| v == version);
    if !tuple_ok {
        let detail = format!(
            "tuple lumen_version {:?} does not match installed {installed_string}",
            tuple.lumen_version
        );
        return ReleaseGateDecision::Refused(ReleaseGateReason::InvalidReleaseTuple {
            version: installed_string,
            detail,
        });
    }
    if let Some(lock) = source_lock_json
        && let Err(err) = tuple.verify_lock_digest(lock)
    {
        return ReleaseGateDecision::Refused(ReleaseGateReason::InvalidReleaseTuple {
            version: installed_string,
            detail: err.to_string(),
        });
    }
    ReleaseGateDecision::Allowed
}

/// Wiring helper for the product update entry points: no release tuple
/// source exists in-tree yet, so for the Lumen line this always fails closed
/// until the release manifest/installer supplies one.
pub fn require_lumen_update_authority() -> Result<(), ReleaseGateReason> {
    let installed = xai_grok_version::installed();
    match release_gate_decision(&installed, None, None) {
        ReleaseGateDecision::Allowed => Ok(()),
        ReleaseGateDecision::Refused(reason) => Err(reason),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::release_tuple::{
        RELEASE_CONTRACT_REVISION, RELEASE_TUPLE_SCHEMA_V1, ReleaseSourceTupleV1,
    };

    fn tuple_for(version: &str) -> ReleaseSourceTupleV1 {
        ReleaseSourceTupleV1 {
            schema: RELEASE_TUPLE_SCHEMA_V1.to_string(),
            source_commit: "a".repeat(40),
            evidence_commit: "b".repeat(40),
            release_tag: format!("v{version}"),
            lumen_version: version.to_string(),
            source_lock_sha256: "c".repeat(64),
            release_contract_revision: RELEASE_CONTRACT_REVISION.to_string(),
        }
    }

    #[test]
    fn pre_rc_lumen_fails_closed_without_tuple() {
        assert_eq!(
            release_gate_decision("2.0.0-alpha.1", None, None),
            ReleaseGateDecision::Refused(ReleaseGateReason::PreRc {
                version: "2.0.0-alpha.1".to_string()
            })
        );
        assert_eq!(
            release_gate_decision("2.0.0-beta.2", None, None),
            ReleaseGateDecision::Refused(ReleaseGateReason::PreRc {
                version: "2.0.0-beta.2".to_string()
            })
        );
    }

    #[test]
    fn grok_identity_lane_is_outside_lumen_authority() {
        assert_eq!(release_gate_decision("0.2.7", None, None), ReleaseGateDecision::Allowed);
        assert_eq!(release_gate_decision("0.1.220-alpha.4", None, None), ReleaseGateDecision::Allowed);
        assert_eq!(release_gate_decision("1.4.0", None, None), ReleaseGateDecision::Allowed);
    }

    #[test]
    fn rc_and_formal_lumen_require_a_tuple() {
        for version in ["2.0.0-rc.1", "2.0.0"] {
            assert_eq!(
                release_gate_decision(version, None, None),
                ReleaseGateDecision::Refused(ReleaseGateReason::MissingReleaseTuple {
                    version: version.to_string()
                })
            );
        }
    }

    #[test]
    fn valid_tuple_allows_rc_and_formal_release() {
        assert_eq!(
            release_gate_decision("2.0.0-rc.1", Some(&tuple_for("2.0.0-rc.1")), None),
            ReleaseGateDecision::Allowed
        );
        assert_eq!(
            release_gate_decision("2.0.0", Some(&tuple_for("2.0.0")), None),
            ReleaseGateDecision::Allowed
        );
    }

    #[test]
    fn tuple_version_mismatch_is_rejected() {
        let tuple = tuple_for("2.0.0-rc.2");
        assert!(matches!(
            release_gate_decision("2.0.0-rc.1", Some(&tuple), None),
            ReleaseGateDecision::Refused(ReleaseGateReason::InvalidReleaseTuple { .. })
        ));
    }

    #[test]
    fn tuple_lock_digest_mismatch_is_rejected_when_lock_supplied() {
        let lock = br#"{"schema_version":1,"lumen_version":"2.0.0-rc.1"}"#;
        let tuple = tuple_for("2.0.0-rc.1"); // sha "cccc..." != digest of lock
        assert!(matches!(
            release_gate_decision("2.0.0-rc.1", Some(&tuple), Some(lock)),
            ReleaseGateDecision::Refused(ReleaseGateReason::InvalidReleaseTuple { .. })
        ));
        // With the correct digest the gate passes.
        let digest = {
            use sha2::{Digest, Sha256};
            let digest = Sha256::digest(lock);
            let mut out = String::with_capacity(64);
            for byte in digest {
                out.push_str(&format!("{byte:02x}"));
            }
            out
        };
        let matching = ReleaseSourceTupleV1 {
            source_lock_sha256: digest,
            ..tuple
        };
        assert_eq!(
            release_gate_decision("2.0.0-rc.1", Some(&matching), Some(lock)),
            ReleaseGateDecision::Allowed
        );
    }

    #[test]
    fn structurally_invalid_tuple_is_rejected() {
        let mut tuple = tuple_for("2.0.0-rc.1");
        tuple.source_commit = tuple.evidence_commit.clone(); // A == B
        assert!(matches!(
            release_gate_decision("2.0.0-rc.1", Some(&tuple), None),
            ReleaseGateDecision::Refused(ReleaseGateReason::InvalidReleaseTuple { .. })
        ));
    }

    #[test]
    fn unparseable_version_fails_closed() {
        assert_eq!(
            release_gate_decision("dev-build", Some(&tuple_for("2.0.0-rc.1")), None),
            ReleaseGateDecision::Refused(ReleaseGateReason::UnverifiableVersion {
                version: "dev-build".to_string()
            })
        );
    }

    #[test]
    fn product_line_discriminator() {
        assert!(is_lumen_product_version("2.0.0-alpha.1"));
        assert!(is_lumen_product_version("2.1.0"));
        assert!(!is_lumen_product_version("0.2.116"));
        assert!(!is_lumen_product_version("1.2.3"));
        assert!(!is_lumen_product_version("garbage"));
    }

    #[test]
    fn pre_rc_reason_message_explains_fail_closed() {
        let reason = ReleaseGateReason::PreRc {
            version: "2.0.0-alpha.1".to_string(),
        };
        assert!(reason.message().contains("fail-closed"));
        assert!(reason.message().contains("v2.0.0-rc.1"));
    }
}
