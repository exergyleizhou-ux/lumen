//! Top-level enum definitions for `acp_session`; their `impl` blocks and
//! helpers stay in `acp_session.rs`.

use super::*;

/// Controls how MCP server system-reminders are injected.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum McpReminderMode {
    /// Only inject what changed (added/removed/updated servers).
    Delta,
    /// Inject the full server list when anything changes.
    Full,
}

/// P0 no-replay baseline for ordinary sampler turns.
///
/// In-process retry is only legal when a sealed attempt receipt proves
/// `NoOutput + NoToolCall + NotAttempted + NoExternalEffect` (see
/// `xai_grok_memory::may_in_process_retry`). Until every transport path
/// produces that receipt, ordinary turns keep max retries at zero and surface
/// failure instead of replaying in-process.
///
/// Prefer [`ordinary_turn_max_retries`] / [`crate::session::nextgen_control::ordinary_sampler_max_retries`]
/// at call sites that already hold an optional seal; this constant is the
/// hard ceiling when no receipt is available.
pub(crate) const NO_RECEIPT_MAX_RETRIES: u32 = 0;

#[cfg(test)]
mod no_replay_policy_tests {
    use super::NO_RECEIPT_MAX_RETRIES;
    use xai_grok_memory::{
        clean_preflight_receipt, mark_output_emitted, may_in_process_retry,
        ordinary_turn_max_retries,
    };

    #[test]
    fn ordinary_turn_retries_stay_zero_and_seal_gate_matches_policy() {
        assert_eq!(
            NO_RECEIPT_MAX_RETRIES, 0,
            "ordinary turns must not in-process retry without sealed receipt"
        );
        assert_eq!(ordinary_turn_max_retries(None), NO_RECEIPT_MAX_RETRIES);
        assert_eq!(
            ordinary_turn_max_retries(Some(&clean_preflight_receipt("t0"))),
            NO_RECEIPT_MAX_RETRIES,
            "clean seal does not raise budget until durable multi-transport store"
        );
        // Clean preflight is the only seal that would permit a *future* retry
        // path once durable receipts are wired; today max_retries stays 0.
        assert!(may_in_process_retry(&clean_preflight_receipt("t0")).is_ok());
        assert!(
            may_in_process_retry(&mark_output_emitted(clean_preflight_receipt("t1"))).is_err(),
            "any emitted output must forbid in-process retry"
        );
    }
}

/// Outcome of `process_conversation_turn`, distinguishing normal completion from cancellation.
pub(crate) enum TurnOutcome {
    /// The model finished responding (no more tool calls).
    /// Carries the turn-end signals snapshot for trace metadata enrichment,
    /// the set of tool names invoked during this turn (for completion
    /// requirement tracking), and the schema-validated `--json-schema` output
    /// (`None` without a schema; `Some(Err)` on parse/validation failure).
    Completed {
        snapshot: Box<Option<TurnDeltaSnapshot>>,
        tools_called: Vec<String>,
        structured_output: Option<Result<serde_json::Value, String>>,
        /// `Some(explanation)` marks a content-filter refusal (empty when the
        /// provider gave no message).
        refusal: Option<String>,
    },
    /// The turn was cancelled (user rejection, hook denial, doom loop, etc.).
    /// The category distinguishes the cause for analytics.
    Cancelled {
        category: Option<crate::session::events::CancellationCategory>,
        context: Option<serde_json::Value>,
    },
    /// The `--max-turns` limit was reached after a tool-execution cycle.
    MaxTurnsReached { limit: usize },
    /// Silent EndTurn after stationarity/true-noop thrash. Distinct from
    /// Completed so recovery/goal/stop-hook cannot re-open the sampling loop.
    StationarityEnded {
        snapshot: Box<Option<TurnDeltaSnapshot>>,
    },
}

#[derive(Debug)]
pub(crate) enum ToolLoop {
    Continue,
    NonExistingTool,
    ToolParsingError,
    /// User clicked "No" on a permission prompt. Carries the rejection reason
    /// and tool name so callers (e.g. subagent result) can report what was rejected.
    PermissionReject {
        tool_name: String,
        reason: String,
    },
    /// The user cancelled the turn (e.g. Cmd+C during a permission prompt).
    Cancelled,
    /// User provided a followup message instead of approving the tool execution.
    /// The string contains the followup message to be added as a user turn.
    FollowupMessage(String),
    /// A user-configured `pre_tool_use` hook blocked this tool call. Non-terminal:
    /// the deny reason is fed back and the turn continues (see `execute_tool_calls`).
    /// Only `hook_name` is retained, for the per-tool telemetry/annotation.
    HookDenied {
        hook_name: String,
    },
}

/// Why the TodoGate fired. Single variant today; kept as an enum so
/// any new reason has to go through the same `as_str` → `TODO_GATE_*`
/// const mapping.
#[doc(hidden)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TodoGateReason {
    /// The turn ended with pending or unbacked in-progress todos.
    InFlight,
}

/// Outcome of `evaluate_todo_gate`. `Nudge` carries the rendered
/// reminder text AND the typed reason so the producer can emit
/// telemetry without re-deriving the reason from the input.
///
/// Exposed as `pub` solely so the replay-trace integration test in
/// `tests/trace_replay.rs` can match against the decision. Not part of
/// the public API.
#[doc(hidden)]
pub enum TodoGateDecision {
    /// Gate is satisfied — the turn may end.
    Continue,
    /// Inject `reminder` as a `<system-reminder>` and force another turn.
    Nudge {
        reminder: String,
        reason: TodoGateReason,
    },
}

/// Closed reason for why `maybe_fire_laziness_check` aborted before
/// producing a verdict. Maps 1:1 to the `LAZINESS_ABORT_*` telemetry
/// consts via `as_const_str()` — exhaustive match, no wildcard, so a
/// new variant forces a new const + arm. Mirrors the
/// `TodoGateReason` pattern (closed-set producer-side closure).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum LazinessAbortReason {
    UserInput,
    ModelSwitch,
    Timeout,
    ClassifierError,
}

/// Reasons the tolerant classifier-output parser can fail. Each carries
/// enough context to log without leaking the (potentially long) raw
/// response — the caller logs the raw text separately, truncated.
#[derive(Debug)]
pub(crate) enum ClassifierParseError {
    /// Strict JSON, fence-stripped JSON, and brace-extracted JSON all
    /// failed to parse — the response is not recoverable.
    Unparseable,
    /// JSON parsed but `confidence` was outside `[0.0, 1.0]`.
    ConfidenceOutOfRange(f32),
}

/// Outcome of `evaluate_laziness`. `Nudge` carries the category const,
/// confidence, and evidence so the producer can emit telemetry and
/// build the nudge text without re-deriving anything from the input.
#[derive(Debug, Clone, PartialEq)]
pub(crate) enum LazinessDecision {
    Nudge {
        category: crate::session::events::LazinessCategory,
        confidence: f32,
        evidence: String,
    },
    NoNudge {
        category: crate::session::events::LazinessCategory,
        confidence: f32,
        reason: NoNudgeReason,
    },
}

/// Closed set of reasons `evaluate_laziness` returned `NoNudge`. Each
/// maps to a distinct dashboard slice — the consistency test below
/// asserts the producer never invents a new reason without extending
/// this enum.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum NoNudgeReason {
    /// Classifier returned one of the `not_stalled_*` categories.
    NotStalled,
    /// Confidence below the configured (or default) threshold.
    LowConfidence,
    /// Per-session cap was `0` (observation-only) or the cap has been
    /// reached. Both cases mean "fire telemetry but inject no nudge".
    CapExhausted,
    /// Defensive: the caller should not have invoked `evaluate_laziness`
    /// when `cfg.enabled = false`, but if it did, return this rather
    /// than panic.
    FeatureDisabled,
}

/// Distinguishes turn-end drains from mid-turn drains. Turn-end is the
/// safe boundary to fire the verification stage (no model
/// inference in flight); mid-turn `update_goal(completed: true)` calls
/// are deferred so the verifier-skeptic subagents never race the
/// parent's sampler.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum DrainPurpose {
    /// End-of-turn drain. Verification-eligible completions fire the
    /// verification stage (if enabled, Active, and not already in flight).
    /// Deferred completions from prior mid-turn drains are processed
    /// FIFO ahead of the regular channel.
    TurnEnd,
    /// Mid-turn drain (e.g. on `SubagentSpawned`). Verification-
    /// eligible completions are deferred to
    /// `pending_classifier_completions` for processing at the next
    /// turn-end.
    MidTurn,
}

/// Origin of a drain entry. `Pending` entries had their acks resolved
/// at defer time; `Channel` entries still carry a live oneshot.
pub(crate) enum DrainSource {
    Pending(xai_grok_tools::implementations::grok_build::update_goal::UpdateGoalInput),
    Channel(xai_grok_tools::implementations::grok_build::update_goal::UpdateGoalEnvelope),
}

/// Reason a NotAchieved verdict was synthesized without invoking the
/// sampler. Used by `account_not_achieved_without_sampler` to label
/// the synthetic details file and (in future variants) emit distinct
/// telemetry.
#[derive(Debug, Clone, Copy)]
pub(crate) enum NotAchievedSyntheticReason {
    /// Guard 3 — a second `update_goal(completed: true)` arrived while
    /// a classifier was already running for this goal.
    ConcurrentInFlight,
}

/// Decision for one in-turn goal round (see
/// [`SessionActor::run_goal_round_end`]): keep the turn alive and run another
/// round with `directive` injected, or end the turn because the goal resolved
/// (achieved / paused / blocked) or this isn't a goal turn.
pub(crate) enum GoalRoundDecision {
    Continue(String),
    EndTurn,
}

/// Decision from the turn-end stop gate: allow the turn to end, or keep the
/// agent working by injecting `feedback` as a synthetic user message.
#[derive(Debug)]
pub(crate) enum StopGateDecision {
    AllowStop,
    KeepWorking { feedback: String },
}

/// Which part of the model's streaming lifecycle the capture was tied to
/// when it was last touched — i.e. what the model was doing at the moment
/// the turn was cut off. Serialized onto `streaming_partial.json` so trace
/// inspection can tell whether the abort interrupted thinking, the visible
/// response, or tool-call emission.
///
/// There is deliberately **no `ToolExecution` variant**: on
/// `SamplingEvent::Completed` (the point at which the canonical assistant
/// message is committed) the in-progress generation is discarded from the
/// capture, and `Completed` always fires before the turn loop dispatches
/// tools. A streaming partial can therefore only ever be tied to one of the
/// model's own emission phases below — never to tool execution, whose
/// interruptions are covered by the canonical `record_assistant_response`
/// path instead.
#[derive(Debug, Default, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub(crate) enum CapturePhase {
    /// Stream opened (`StreamStarted`) but no channel token observed yet.
    #[default]
    Pending,
    /// Reasoning ("thinking") tokens were the most recent output.
    Reasoning,
    /// Visible response-text tokens were the most recent output.
    ResponseText,
    /// The model had begun emitting a tool call (`ToolCallDelta`).
    ToolCall,
}
