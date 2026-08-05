//! Dual-model planner/executor contract (DEBT-033 C1).
//!
//! The executor (DeepSeek V4 Flash) does the work; a planner (higher-tier
//! model, e.g. DeepSeek-V4-Pro) may produce the plan in a SEPARATE session —
//! sessions never mix, so neither model's prefix cache is disturbed
//! (INV-PE-01/02, Reasonix-class cache-stability). The handoff is a
//! machine-parseable `StructuredPlan`, never free text (INV-PE-02).
//!
//! This module is the pure contract core: types, parsing/validation, the
//! read-only tool filter, and config. The orchestration hook is the existing
//! goal-planner path (`goal.planner_model` / `resolve_goal_planner_model`):
//! the planner session emits `StructuredPlan`, the executor session accepts it
//! via the journal (`PlanSubmitted` / `PlanAccepted`), and any cross-session
//! message leak is a `CrossSessionLeakDetected` fail-closed event (INV-PE-03).

use serde::{Deserialize, Serialize};

/// Bounds keep the plan machine-parseable and budget-bounded.
pub const MAX_PLAN_STEPS: usize = 16;
pub const MAX_SUCCESS_CRITERIA: usize = 8;
pub const MAX_COMPLEXITY: u8 = 10;

/// One step of a structured plan.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PlanStep {
    pub id: String,
    pub action: String,
    #[serde(default)]
    pub tools: Vec<String>,
    #[serde(default)]
    pub expected_outcome: String,
}

/// The machine-parseable handoff from planner to executor.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct StructuredPlan {
    pub goal_id: String,
    pub steps: Vec<PlanStep>,
    pub success_criteria: Vec<String>,
    /// 1..=10; drives effort policy (A3 escalation signal).
    pub estimated_complexity: u8,
}

impl StructuredPlan {
    /// Parse + validate a planner-produced JSON plan. Rejects empty goals,
    /// empty/oversized step lists, empty criteria, and out-of-range
    /// complexity — a malformed plan fails closed (never handed to the
    /// executor as free text).
    pub fn parse(json: &str) -> Result<Self, PlanValidationError> {
        let plan: StructuredPlan = serde_json::from_str(json).map_err(|e| {
            PlanValidationError::MalformedJson(e.to_string())
        })?;
        plan.validate()?;
        Ok(plan)
    }

    pub fn validate(&self) -> Result<(), PlanValidationError> {
        if self.goal_id.trim().is_empty() {
            return Err(PlanValidationError::EmptyGoal);
        }
        if self.steps.is_empty() || self.steps.len() > MAX_PLAN_STEPS {
            return Err(PlanValidationError::StepCount(self.steps.len()));
        }
        if self.success_criteria.is_empty() || self.success_criteria.len() > MAX_SUCCESS_CRITERIA {
            return Err(PlanValidationError::CriteriaCount(self.success_criteria.len()));
        }
        if !(1..=MAX_COMPLEXITY).contains(&self.estimated_complexity) {
            return Err(PlanValidationError::ComplexityOutOfRange(
                self.estimated_complexity,
            ));
        }
        for step in &self.steps {
            if step.id.trim().is_empty() || step.action.trim().is_empty() {
                return Err(PlanValidationError::EmptyStep);
            }
        }
        Ok(())
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PlanValidationError {
    MalformedJson(String),
    EmptyGoal,
    StepCount(usize),
    CriteriaCount(usize),
    ComplexityOutOfRange(u8),
    EmptyStep,
}

impl std::fmt::Display for PlanValidationError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::MalformedJson(e) => write!(f, "plan.malformed_json: {e}"),
            Self::EmptyGoal => write!(f, "plan.empty_goal"),
            Self::StepCount(n) => write!(f, "plan.step_count_out_of_range: {n}"),
            Self::CriteriaCount(n) => write!(f, "plan.criteria_count_out_of_range: {n}"),
            Self::ComplexityOutOfRange(n) => write!(f, "plan.complexity_out_of_range: {n}"),
            Self::EmptyStep => write!(f, "plan.empty_step"),
        }
    }
}

/// Keep only tools the planner session may use: strictly read-only surface
/// (INV-PE-04). The planner never holds writer/destructive tools, so it
/// cannot mutate or leak effects into the executor session.
pub fn planner_readonly_tools<'a>(
    tools: impl IntoIterator<Item = (&'a str, bool)>,
) -> Vec<&'a str> {
    tools
        .into_iter()
        .filter(|(_, read_only)| *read_only)
        .map(|(name, _)| name)
        .collect()
}

/// Dual-model configuration (C1). `executor` is the working model (flash);
/// `planner` is the higher-tier planning model. `plan_format` is fixed to
/// structured JSON by this contract.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct DualSessionConfig {
    pub planner_model: String,
    pub executor_model: String,
    pub plan_format: PlanFormat,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum PlanFormat {
    StructuredJson,
}

/// The DeepSeek-first default preset: flash executes, pro plans.
pub fn deepseek_flash_executor_pro_planner() -> DualSessionConfig {
    DualSessionConfig {
        planner_model: "deepseek-v4-pro".into(),
        executor_model: "deepseek-v4-flash".into(),
        plan_format: PlanFormat::StructuredJson,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn valid_json() -> &'static str {
        r#"{
            "goal_id": "g-1",
            "steps": [
                {"id": "s1", "action": "read failing test", "tools": ["read_file"], "expected_outcome": "understand failure"},
                {"id": "s2", "action": "fix the bug", "tools": ["search_replace"], "expected_outcome": "tests pass"}
            ],
            "success_criteria": ["go test ./... passes"],
            "estimated_complexity": 4
        }"#
    }

    #[test]
    fn parses_and_validates_wellformed_plan() {
        let plan = StructuredPlan::parse(valid_json()).unwrap();
        assert_eq!(plan.goal_id, "g-1");
        assert_eq!(plan.steps.len(), 2);
        assert_eq!(plan.estimated_complexity, 4);
    }

    #[test]
    fn rejects_malformed_and_out_of_bounds_plans() {
        assert!(matches!(
            StructuredPlan::parse("not json"),
            Err(PlanValidationError::MalformedJson(_))
        ));
        assert_eq!(
            StructuredPlan::parse(r#"{"goal_id":"","steps":[{"id":"s","action":"a"}],"success_criteria":["c"],"estimated_complexity":1}"#),
            Err(PlanValidationError::EmptyGoal)
        );
        assert!(matches!(
            StructuredPlan::parse(r#"{"goal_id":"g","steps":[],"success_criteria":["c"],"estimated_complexity":1}"#),
            Err(PlanValidationError::StepCount(0))
        ));
        assert_eq!(
            StructuredPlan::parse(r#"{"goal_id":"g","steps":[{"id":"s","action":"a"}],"success_criteria":["c"],"estimated_complexity":0}"#),
            Err(PlanValidationError::ComplexityOutOfRange(0))
        );
        assert_eq!(
            StructuredPlan::parse(r#"{"goal_id":"g","steps":[{"id":"s","action":"a"}],"success_criteria":["c"],"estimated_complexity":11}"#),
            Err(PlanValidationError::ComplexityOutOfRange(11))
        );
        // Empty step action fails closed.
        assert!(matches!(
            StructuredPlan::parse(r#"{"goal_id":"g","steps":[{"id":"s","action":" "}],"success_criteria":["c"],"estimated_complexity":1}"#),
            Err(PlanValidationError::EmptyStep)
        ));
    }

    #[test]
    fn oversized_plan_is_rejected() {
        let steps: Vec<serde_json::Value> = (0..=MAX_PLAN_STEPS)
            .map(|i| serde_json::json!({"id": format!("s{i}"), "action": "a"}))
            .collect();
        let json = serde_json::json!({
            "goal_id": "g",
            "steps": steps,
            "success_criteria": ["c"],
            "estimated_complexity": 1,
        })
        .to_string();
        assert!(matches!(
            StructuredPlan::parse(&json),
            Err(PlanValidationError::StepCount(n)) if n == MAX_PLAN_STEPS + 1
        ));
    }

    #[test]
    fn planner_only_gets_readonly_tools() {
        let tools = vec![
            ("read_file", true),
            ("grep", true),
            ("search_replace", false),
            ("run_terminal_command", false),
            ("list_dir", true),
        ];
        let filtered = planner_readonly_tools(tools);
        assert_eq!(filtered, vec!["read_file", "grep", "list_dir"]);
    }

    #[test]
    fn deepseek_preset_config() {
        let cfg = deepseek_flash_executor_pro_planner();
        assert_eq!(cfg.executor_model, "deepseek-v4-flash");
        assert_eq!(cfg.planner_model, "deepseek-v4-pro");
        assert_eq!(cfg.plan_format, PlanFormat::StructuredJson);
        let bytes = serde_json::to_vec(&cfg).unwrap();
        assert!(bytes.len() > 10);
    }
}
