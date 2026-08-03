pub mod auto_update;
pub mod release_gate;
pub mod release_tuple;
pub mod version;
mod version_policy;

pub use auto_update::UpdateStatus;
pub use release_gate::{
    ReleaseGateDecision, ReleaseGateReason, is_lumen_product_version, release_gate_decision,
    require_lumen_update_authority,
};
pub use release_tuple::{
    ReleaseSourceTupleV1, ReleaseTupleError, RELEASE_CONTRACT_REVISION,
    RELEASE_TUPLE_SCHEMA_V1, is_evidence_only_path,
};
pub use version::{UpdateConfig, channel_label, channel_name, write_version_cache};
pub use version_policy::enforce_version_policy_or_exit;
