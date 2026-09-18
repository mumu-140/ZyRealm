package model

// AttemptRoutingTrace is embedded into ChannelAttempt's JSON payload. Keeping
// routing observability inside the existing serialized attempts avoids a schema
// migration while making one routing decision inspectable end-to-end.
type AttemptRoutingTrace struct {
	DecisionTraceVersion string                 `json:"decision_trace_version,omitempty"`
	CredentialRevision   int                    `json:"credential_revision,omitempty"`
	FailureDomain        string                 `json:"failure_domain,omitempty"`
	FailureScope         string                 `json:"failure_scope,omitempty"`
	RuleID               string                 `json:"rule_id,omitempty"`
	RetryDirective       string                 `json:"retry_directive,omitempty"`
	FailoverStopReason   string                 `json:"failover_stop_reason,omitempty"`
	RuntimeEffect        string                 `json:"runtime_effect,omitempty"`
	RuntimeState         string                 `json:"runtime_state,omitempty"`
	CooldownUntil        int64                  `json:"cooldown_until,omitempty"`
	RouteLearningEffect  string                 `json:"route_learning_effect,omitempty"`
	CircuitEffect        string                 `json:"circuit_effect,omitempty"` // Deprecated: decode compatibility for historical attempt JSON.
	OutlierEffect        string                 `json:"outlier_effect,omitempty"`
	ReplaySafety         string                 `json:"replay_safety,omitempty"`
	DispatchState        string                 `json:"dispatch_state,omitempty"`
	DownstreamCommitted  bool                   `json:"downstream_committed,omitempty"`
	OuterContextState    string                 `json:"outer_context_state,omitempty"`
	OutboundContextCause string                 `json:"outbound_context_cause,omitempty"`
	ProviderAttempt      int                    `json:"provider_attempt,omitempty"`
	WireAttempt          int                    `json:"wire_attempt,omitempty"`
	DecisionEvents       []RoutingDecisionEvent `json:"decision_events,omitempty"`
}
