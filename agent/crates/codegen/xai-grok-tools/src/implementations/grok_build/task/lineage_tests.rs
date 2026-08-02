//! Pure unit tests for `SubagentLineage` construction, validation, and
//! serialization.  No coordinator, no runner: these pin the tree-placement
//! contract that the coordinator and the shell prompt renderer rely on.
//!
//! NG-01 slice: lineage serialization/validation pure tests (round-trip,
//! forged/cycle/root-mismatch rejection, legacy root-only decode).

use crate::implementations::grok_build::task::types::SubagentLineage;

#[test]
fn direct_lineage_round_trips_through_serialization() {
    let lineage = SubagentLineage::direct("root");
    let json = serde_json::to_string(&lineage).expect("serialize");
    let decoded: SubagentLineage = serde_json::from_str(&json).expect("deserialize");
    assert_eq!(decoded, lineage);
    // Field-level pin so a future reorder/rename cannot silently change the
    // wire form.
    let value: serde_json::Value = serde_json::from_str(&json).expect("json");
    assert_eq!(value["root_session_id"], "root");
    assert_eq!(value["immediate_parent_session_id"], "root");
    assert_eq!(value["depth"], 1);
    assert_eq!(value["lineage_path"], serde_json::json!(["root"]));
}

#[test]
fn legacy_root_only_record_decodes_to_root_only_projection() {
    // A record written before depth/lineage_path existed: only the root
    // identity survives.  Decodes without error and without inventing
    // ancestor information.
    let json = r#"{"root_session_id":"root","immediate_parent_session_id":"root"}"#;
    let decoded: SubagentLineage = serde_json::from_str(json).expect("legacy decode");
    assert_eq!(decoded.root_session_id, "root");
    assert_eq!(decoded.immediate_parent_session_id, "root");
    assert_eq!(decoded.depth, 1);
    assert!(
        decoded.lineage_path.is_empty(),
        "legacy records have no ancestor path"
    );
}

#[test]
fn child_of_extends_path_and_depth() {
    let root = SubagentLineage::direct("root");
    let child = SubagentLineage::child_of(&root, "child");
    assert_eq!(child.root_session_id, "root");
    assert_eq!(child.immediate_parent_session_id, "child");
    assert_eq!(child.depth, 2);
    assert_eq!(child.lineage_path, vec!["root", "child"]);

    let grandchild = SubagentLineage::child_of(&child, "grandchild");
    assert_eq!(grandchild.root_session_id, "root");
    assert_eq!(grandchild.immediate_parent_session_id, "grandchild");
    assert_eq!(grandchild.depth, 3);
    assert_eq!(grandchild.lineage_path, vec!["root", "child", "grandchild"]);
}

#[test]
fn child_of_does_not_duplicate_same_parent() {
    let root = SubagentLineage::direct("root");
    let child = SubagentLineage::child_of(&root, "child");
    // Re-spawning from the same parent must not duplicate the path entry.
    let again = SubagentLineage::child_of(&child, "child");
    assert_eq!(again.lineage_path, vec!["root", "child"]);
    assert_eq!(again.depth, 3, "depth still advances per generation");
}

#[test]
fn child_of_from_ancestor_documents_cycle_hazard() {
    // The coordinator only ever rebuilds nested lineage from the registered
    // active parent (matched by child_session_id), so a spawn whose
    // parent_session_id is an *ancestor* rather than the direct parent cannot
    // reach child_of in production.  This pins the pure-function invariant:
    // an ancestor id duplicates the path entry instead of silently
    // truncating, so any future caller that forgets the lookup sees the
    // anomaly in the path rather than a flat, plausible-looking tree.
    let root = SubagentLineage::direct("root");
    let child = SubagentLineage::child_of(&root, "child");
    let grandchild = SubagentLineage::child_of(&child, "grandchild");
    let forged = SubagentLineage::child_of(&grandchild, "root"); // ancestor id
    assert_eq!(forged.lineage_path, vec!["root", "child", "grandchild", "root"]);
}

#[test]
fn validate_direct_accepts_wellformed_direct_lineage() {
    let lineage = SubagentLineage::direct("root");
    assert_eq!(lineage.validate_direct_for("root"), Ok(()));
}

#[test]
fn validate_direct_rejects_forged_root() {
    let mut lineage = SubagentLineage::direct("root");
    lineage.root_session_id = "other".to_owned();
    assert_eq!(
        lineage.validate_direct_for("root"),
        Err("direct child root_session_id must equal parent_session_id")
    );
}

#[test]
fn validate_direct_rejects_immediate_parent_mismatch() {
    let mut lineage = SubagentLineage::direct("root");
    lineage.immediate_parent_session_id = "other".to_owned();
    assert_eq!(
        lineage.validate_direct_for("root"),
        Err("direct child immediate_parent_session_id must equal parent_session_id")
    );
}

#[test]
fn validate_direct_rejects_forged_depth() {
    let mut lineage = SubagentLineage::direct("root");
    lineage.depth = 2;
    assert_eq!(
        lineage.validate_direct_for("root"),
        Err("direct child depth must be 1")
    );
}

#[test]
fn validate_direct_rejects_forged_or_cyclic_path() {
    // Extra ancestor element: a direct child cannot claim a forged ancestry.
    let mut lineage = SubagentLineage::direct("root");
    lineage.lineage_path = vec!["root".to_owned(), "forged".to_owned()];
    assert_eq!(
        lineage.validate_direct_for("root"),
        Err("direct child lineage_path must contain only parent_session_id")
    );

    // Self-cycle attempt: the path repeats the parent id.
    let mut lineage = SubagentLineage::direct("root");
    lineage.lineage_path = vec!["root".to_owned(), "root".to_owned()];
    assert!(lineage.validate_direct_for("root").is_err());
}

#[test]
fn validate_direct_rejects_empty_and_whitespace_parent() {
    assert_eq!(
        SubagentLineage::direct("").validate_direct_for(""),
        Err("parent session id must not be empty")
    );
    assert_eq!(
        SubagentLineage::direct("  ").validate_direct_for("  "),
        Err("parent session id must not be empty")
    );
}
