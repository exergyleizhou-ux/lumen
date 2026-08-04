//! Governed-tree overlay (M1 product surface, DEBT-024(c)).
//!
//! Draws the governed task-tree projection from `x.ai/governedTree/status`
//! (shell ACP seam) as an overlay panel. The pane renders only what the
//! projection carries — truthful node capabilities, typed denies, and the
//! profile upgrade surface — never model prose or fabricated state.

use super::AgentView;
use crate::app::app_view::InputOutcome;
use crossterm::event::{Event, KeyCode, KeyEventKind};

impl AgentView {
    /// Feed the projection from `x.ai/governedTree/status`.
    pub(crate) fn set_governed_tree_status(
        &mut self,
        status: crate::views::governed_tree::GovernedTreeStatusWire,
    ) {
        self.governed_tree_status = Some(status);
    }

    /// Toggle the overlay (no-op until a projection is present).
    pub(crate) fn toggle_governed_tree(&mut self) {
        if self.governed_tree_status.is_some() {
            self.show_governed_tree = !self.show_governed_tree;
        }
    }

    pub(super) fn handle_governed_tree_overlay_input(
        &mut self,
        ev: &Event,
    ) -> Option<InputOutcome> {
        if self.show_governed_tree {
            if let Event::Key(key) = ev
                && key.kind != KeyEventKind::Release
            {
                match key.code {
                    KeyCode::Esc | KeyCode::Char('q') => {
                        self.show_governed_tree = false;
                        Some(InputOutcome::Changed)
                    }
                    _ => None,
                }
            } else {
                None
            }
        } else {
            None
        }
    }
}
