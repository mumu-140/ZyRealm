package model

const RoutingDecisionTraceVersion = "routing-decisions-v1"

// RoutingDecisionStage identifies where a routing fact was observed. These
// values are persisted in RelayLog.Attempts and therefore form a stable API
// contract for historical diagnostics.
type RoutingDecisionStage string

const (
	DecisionStageCandidate  RoutingDecisionStage = "candidate"
	DecisionStageCredential RoutingDecisionStage = "credential"
	DecisionStageProtocol   RoutingDecisionStage = "protocol"
	DecisionStageDispatch   RoutingDecisionStage = "dispatch"
	DecisionStageFailover   RoutingDecisionStage = "failover"
)

// RoutingDecisionOutcome describes the result of one observed routing fact.
type RoutingDecisionOutcome string

const (
	DecisionOutcomeEligible  RoutingDecisionOutcome = "eligible"
	DecisionOutcomeRejected  RoutingDecisionOutcome = "rejected"
	DecisionOutcomeSelected  RoutingDecisionOutcome = "selected"
	DecisionOutcomeAttempted RoutingDecisionOutcome = "attempted"
	DecisionOutcomeStopped   RoutingDecisionOutcome = "stopped"
)

// RoutingDecisionReason is a machine-readable reason code. Keep this list
// intentionally small; free-form upstream payloads and secrets do not belong in
// the routing explanation contract.
type RoutingDecisionReason string

const (
	DecisionReasonRuntimeCooldown      RoutingDecisionReason = "runtime_cooldown"
	DecisionReasonRuntimeSuspect       RoutingDecisionReason = "runtime_suspect"
	DecisionReasonCredentialCooldown   RoutingDecisionReason = "credential_cooldown"
	DecisionReasonCapabilityNegative   RoutingDecisionReason = "capability_negative"
	DecisionReasonCapacity             RoutingDecisionReason = "capacity"
	DecisionReasonRateLimit            RoutingDecisionReason = "rate_limit"
	DecisionReasonProtocolIncompatible RoutingDecisionReason = "protocol_incompatible"
	DecisionReasonAttemptBudget        RoutingDecisionReason = "attempt_budget"
	DecisionReasonReplayUnsafe         RoutingDecisionReason = "replay_unsafe"
)

// RoutingDecisionEvent is a bounded, non-sensitive historical routing fact.
// It deliberately excludes credential values, headers, prompts, request bodies,
// and response bodies.
type RoutingDecisionEvent struct {
	Sequence     int                    `json:"sequence"`
	Stage        RoutingDecisionStage   `json:"stage"`
	Outcome      RoutingDecisionOutcome `json:"outcome"`
	Reason       RoutingDecisionReason  `json:"reason,omitempty"`
	ChannelID    int                    `json:"channel_id,omitempty"`
	ChannelKeyID int                    `json:"channel_key_id,omitempty"`
	ChannelName  string                 `json:"channel_name,omitempty"`
	ModelName    string                 `json:"model_name,omitempty"`
	Protocol     string                 `json:"protocol,omitempty"`
	ExpiresAt    int64                  `json:"expires_at,omitempty"`
	Detail       string                 `json:"detail,omitempty"`
}
