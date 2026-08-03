//! NG-04D-4 consumer-facing sandbox capability resource (tools side).
//!
//! Full [`AgentSandboxV1`] lives in memory (crate cycle prevention). Hosts
//! project the enforcement bits into this resource so writers and spawn
//! checks can fail closed without depending on memory.

use serde::{Deserialize, Serialize};

use crate::register_resource;

/// Host-injected capability projection for a nested child session.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ChildSandboxCapability {
    pub node_id: String,
    pub root_tree_id: String,
    pub depth: u32,
    pub branch_id: String,
    pub accepted_snapshot_hash: String,
    pub may_spawn: bool,
    pub may_write: bool,
    pub may_network: bool,
    pub revoked: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ChildSandboxDeny {
    Revoked,
    SpawnDenied,
    WriteDenied,
    NetworkDenied,
}

impl ChildSandboxDeny {
    pub const fn code(self) -> &'static str {
        match self {
            Self::Revoked => "child_sandbox.revoked",
            Self::SpawnDenied => "child_sandbox.spawn_denied",
            Self::WriteDenied => "child_sandbox.write_denied",
            Self::NetworkDenied => "child_sandbox.network_denied",
        }
    }
}

impl ChildSandboxCapability {
    /// Leaf-safe defaults for governed children without a richer sandbox.
    pub fn governed_defaults(
        node_id: impl Into<String>,
        root_tree_id: impl Into<String>,
        depth: u32,
        hard_max_depth: u32,
    ) -> Self {
        let is_leaf = depth >= hard_max_depth;
        Self {
            node_id: node_id.into(),
            root_tree_id: root_tree_id.into(),
            depth,
            branch_id: "default".into(),
            accepted_snapshot_hash: String::new(),
            may_spawn: !is_leaf,
            // Governed children default fail-closed on write/network unless
            // host explicitly widens via a non-default constructor.
            may_write: false,
            may_network: false,
            revoked: false,
        }
    }

    pub fn authorize_spawn(&self) -> Result<(), ChildSandboxDeny> {
        if self.revoked {
            return Err(ChildSandboxDeny::Revoked);
        }
        if !self.may_spawn {
            return Err(ChildSandboxDeny::SpawnDenied);
        }
        Ok(())
    }

    pub fn authorize_write(&self) -> Result<(), ChildSandboxDeny> {
        if self.revoked {
            return Err(ChildSandboxDeny::Revoked);
        }
        if !self.may_write {
            return Err(ChildSandboxDeny::WriteDenied);
        }
        Ok(())
    }

    pub fn authorize_network(&self) -> Result<(), ChildSandboxDeny> {
        if self.revoked {
            return Err(ChildSandboxDeny::Revoked);
        }
        if !self.may_network {
            return Err(ChildSandboxDeny::NetworkDenied);
        }
        Ok(())
    }
}

/// Resource wrapper for tool bridge injection.
#[derive(Debug, Clone)]
pub struct ChildSandboxCapabilityResource(pub ChildSandboxCapability);

impl ChildSandboxCapabilityResource {
    pub fn authorize_write(&self) -> Result<(), ChildSandboxDeny> {
        self.0.authorize_write()
    }

    pub fn authorize_spawn(&self) -> Result<(), ChildSandboxDeny> {
        self.0.authorize_spawn()
    }
}

register_resource!(
    "grok_build",
    "ChildSandboxCapabilityResource",
    ChildSandboxCapabilityResource
);

/// When the resource is present, writes must be allowed by the sandbox
/// capability. Missing resource = root/legacy unconstrained (same as write scope).
pub fn enforce_child_sandbox_write_if_present(
    resources: &crate::types::resources::Resources,
) -> Result<(), String> {
    let Some(cap) = resources.get::<ChildSandboxCapabilityResource>() else {
        return Ok(());
    };
    cap.authorize_write()
        .map_err(|d| format!("{}: child sandbox forbids write", d.code()))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::resources::Resources;

    #[test]
    fn leaf_defaults_deny_spawn_write_network() {
        let leaf = ChildSandboxCapability::governed_defaults("leaf", "root", 3, 3);
        assert_eq!(
            leaf.authorize_spawn().unwrap_err(),
            ChildSandboxDeny::SpawnDenied
        );
        assert_eq!(
            leaf.authorize_write().unwrap_err(),
            ChildSandboxDeny::WriteDenied
        );
        assert_eq!(
            leaf.authorize_network().unwrap_err(),
            ChildSandboxDeny::NetworkDenied
        );
    }

    #[test]
    fn enforce_resource_gates_writers_when_injected() {
        let mut resources = Resources::new();
        assert!(enforce_child_sandbox_write_if_present(&resources).is_ok());
        let mut cap = ChildSandboxCapability::governed_defaults("c", "root", 1, 3);
        resources.insert(ChildSandboxCapabilityResource(cap.clone()));
        assert!(enforce_child_sandbox_write_if_present(&resources).is_err());
        cap.may_write = true;
        resources.insert(ChildSandboxCapabilityResource(cap));
        assert!(enforce_child_sandbox_write_if_present(&resources).is_ok());
    }
}
