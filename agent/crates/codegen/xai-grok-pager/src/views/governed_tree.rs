//! Governed-tree pane (M1 product surface, DEBT-024(c)).
//!
//! Renders the governed task-tree projection that the shell exposes through
//! `x.ai/governedTree/status` (offline three-node fixture: root → child →
//! leaf). The wire types mirror the shell response exactly (camelCase), so a
//! client can deserialize the ACP payload directly into this pane. Rendering
//! is a pure line projection (`render_tree_lines`) — unit-testable without a
//! terminal — with a thin ratatui draw on top.

use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, Borders, Paragraph};
use serde::Deserialize;
use unicode_width::UnicodeWidthStr;

/// Node projection wire type (mirrors the shell's `TreeNodeProjectionWire`).
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TreeNodeProjectionWire {
    pub node_id: String,
    pub parent_id: Option<String>,
    pub depth: u8,
    pub branch_id: String,
    pub phase: String,
    pub may_spawn: bool,
    pub may_write: bool,
    pub may_network: bool,
}

/// Deny record wire type (mirrors the shell's `DenyRecordWire`).
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DenyRecordWire {
    pub node_id: String,
    pub action: String,
    pub mechanism: String,
    pub code: String,
}

/// Full `x.ai/governedTree/status` response wire type.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct GovernedTreeStatusWire {
    pub fixture_id: String,
    pub nodes: Vec<TreeNodeProjectionWire>,
    pub denies: Vec<DenyRecordWire>,
    pub accepted_snapshot_hash: String,
    pub default_profile: String,
    pub upgrade_target: String,
    pub upgrade_deny_on_downgrade: String,
}

fn cap_marker(allowed: bool) -> &'static str {
    if allowed {
        "✓"
    } else {
        "✗"
    }
}

/// Truncate a rendered line to the pane width (UTF-8 safe, ellipsis marker).
fn truncate(line: String, width: usize) -> String {
    if UnicodeWidthStr::width(line.as_str()) <= width {
        return line;
    }
    let mut end = line.len();
    while end > 0 && UnicodeWidthStr::width(&line[..end]) > width.saturating_sub(1) {
        end -= 1;
        while end > 0 && !line.is_char_boundary(end) {
            end -= 1;
        }
    }
    format!("{}…", &line[..end])
}

/// Pure line projection of the governed tree — the pane's render core.
///
/// One line per node (indented by depth) with truthful capability markers,
/// a deny section with machine-readable codes, and the profile upgrade
/// surface. No model prose, no fabricated state: only what the projection
/// carries.
pub fn render_tree_lines(status: &GovernedTreeStatusWire, width: usize) -> Vec<String> {
    let width = width.max(40);
    let mut lines = Vec::new();
    lines.push(truncate(format!("Governed tree  {}", status.fixture_id), width));
    lines.push("─".repeat(width.min(72)));
    let mut sorted = status.nodes.clone();
    sorted.sort_by_key(|node| (node.depth, node.node_id.clone()));
    for node in &sorted {
        let indent = "  ".repeat(node.depth as usize);
        lines.push(truncate(
            format!(
                "{indent}• {}  depth={}  spawn={} write={} network={}  phase={}",
                node.node_id,
                node.depth,
                cap_marker(node.may_spawn),
                cap_marker(node.may_write),
                cap_marker(node.may_network),
                node.phase,
            ),
            width,
        ));
    }
    if !status.denies.is_empty() {
        lines.push("─".repeat(width.min(72)));
        for deny in &status.denies {
            lines.push(truncate(
                format!(
                    "  deny: {} {} ({}) — {}",
                    deny.node_id, deny.action, deny.mechanism, deny.code
                ),
                width,
            ));
        }
    }
    lines.push("─".repeat(width.min(72)));
    lines.push(truncate(format!("snapshot {}", status.accepted_snapshot_hash), width));
    lines.push(truncate(
        format!(
            "profile {} → upgrade {} (downgrade: {})",
            status.default_profile, status.upgrade_target, status.upgrade_deny_on_downgrade
        ),
        width,
    ));
    lines
}

/// Thin ratatui wrapper over [`render_tree_lines`].
pub fn render_governed_tree(buf: &mut Buffer, area: Rect, status: &GovernedTreeStatusWire) {
    let lines: Vec<Line<'_>> = render_tree_lines(status, area.width as usize)
        .into_iter()
        .map(|text| Line::from(Span::raw(text)))
        .collect();
    let paragraph = Paragraph::new(lines)
        .block(Block::default().borders(Borders::ALL).title(" governed tree "))
        .scroll((0, 0));
    ratatui::widgets::Widget::render(paragraph, area, buf);
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_status() -> GovernedTreeStatusWire {
        serde_json::from_str(
            r#"{
                "fixtureId": "m1",
                "nodes": [
                    {"nodeId": "root", "parentId": null, "depth": 0, "branchId": "branch-root", "phase": "running", "maySpawn": true, "mayWrite": true, "mayNetwork": true},
                    {"nodeId": "child", "parentId": "root", "depth": 1, "branchId": "branch-child", "phase": "running", "maySpawn": true, "mayWrite": true, "mayNetwork": false},
                    {"nodeId": "leaf", "parentId": "child", "depth": 3, "branchId": "branch-leaf", "phase": "running", "maySpawn": false, "mayWrite": false, "mayNetwork": false}
                ],
                "denies": [
                    {"nodeId": "leaf", "action": "spawn", "mechanism": "lineage_depth", "code": "sandbox.spawn_denied"},
                    {"nodeId": "leaf", "action": "write", "mechanism": "sandbox_enforcement", "code": "sandbox.write_denied"}
                ],
                "acceptedSnapshotHash": "sha256:snap",
                "defaultProfile": "interactive_single_turn",
                "upgradeTarget": "governed_tree_development",
                "upgradeDenyOnDowngrade": "profile.admission_upgrade_failed"
            }"#,
        )
        .expect("wire payload parses")
    }

    #[test]
    fn wire_payload_parses_from_acp_camelcase() {
        let status = sample_status();
        assert_eq!(status.nodes.len(), 3);
        assert_eq!(status.default_profile, "interactive_single_turn");
    }

    #[test]
    fn render_tree_lines_projects_three_node_tree_truthfully() {
        let status = sample_status();
        // Wide enough to render every field untruncated (truncation is
        // covered by the empty-state test).
        let lines = render_tree_lines(&status, 120);
        let text = lines.join("\n");
        // Three nodes, depth-ordered, indented (header + separator first).
        assert!(lines[0].contains("Governed tree"));
        assert!(lines[2].contains("root"), "root first: {}", lines[2]);
        assert!(lines[3].contains("child"));
        assert!(lines[4].contains("leaf"));
        assert!(
            lines[4].starts_with("      • leaf"),
            "leaf indented by 2 spaces per depth (6 for depth 3): {}",
            lines[4]
        );
        // Leaf capabilities are truthfully marked down.
        assert!(lines[4].contains("spawn=✗"), "leaf cannot spawn");
        assert!(lines[4].contains("write=✗"), "leaf cannot write");
        assert!(lines[4].contains("network=✗"), "leaf cannot network");
        assert!(lines[3].contains("network=✗"), "child is network-denied");
        // Deny records carry machine-readable codes.
        assert!(text.contains("sandbox.spawn_denied"));
        assert!(text.contains("lineage_depth"));
        // Profile upgrade surface.
        assert!(text.contains("interactive_single_turn → upgrade governed_tree_development"));
        assert!(text.contains("profile.admission_upgrade_failed"));
    }

    #[test]
    fn render_tree_lines_handles_empty_state() {
        let status = GovernedTreeStatusWire {
            fixture_id: "none".into(),
            nodes: vec![],
            denies: vec![],
            accepted_snapshot_hash: "sha256:none".into(),
            default_profile: "interactive_single_turn".into(),
            upgrade_target: "governed_tree_development".into(),
            upgrade_deny_on_downgrade: "profile.admission_upgrade_failed".into(),
        };
        let lines = render_tree_lines(&status, 40);
        assert!(!lines.is_empty());
        assert!(lines.iter().all(|line| UnicodeWidthStr::width(line.as_str()) <= 72));
    }
}
