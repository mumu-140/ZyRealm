package routinginspect

import (
	"sort"

	"github.com/bestruirui/octopus/internal/model"
)

const ExplanationVersion = "v1"

type Completeness string

const (
	CompletenessComplete      Completeness = "complete"
	CompletenessMixedPartial  Completeness = "mixed_partial"
	CompletenessLegacyPartial Completeness = "legacy_partial"
	CompletenessDecisionOnly  Completeness = "decision_only"
)

type RouteSummary struct {
	AttemptNum   int                 `json:"attempt_num"`
	ChannelID    int                 `json:"channel_id"`
	ChannelKeyID int                 `json:"channel_key_id,omitempty"`
	ChannelName  string              `json:"channel_name,omitempty"`
	ModelName    string              `json:"model_name,omitempty"`
	Status       model.AttemptStatus `json:"status"`
	Duration     int                 `json:"duration_ms,omitempty"`
	Protocol     string              `json:"protocol,omitempty"`
}

type AttemptSummary struct {
	RouteSummary
	FailureDomain        string `json:"failure_domain,omitempty"`
	FailureScope         string `json:"failure_scope,omitempty"`
	RuleID               string `json:"rule_id,omitempty"`
	RetryDirective       string `json:"retry_directive,omitempty"`
	FailoverStopReason   string `json:"failover_stop_reason,omitempty"`
	RuntimeEffect        string `json:"runtime_effect,omitempty"`
	RuntimeState         string `json:"runtime_state,omitempty"`
	CooldownUntil        int64  `json:"cooldown_until,omitempty"`
	CircuitEffect        string `json:"circuit_effect,omitempty"`
	OutlierEffect        string `json:"outlier_effect,omitempty"`
	ReplaySafety         string `json:"replay_safety,omitempty"`
	DispatchState        string `json:"dispatch_state,omitempty"`
	DownstreamCommitted  bool   `json:"downstream_committed,omitempty"`
	OuterContextState    string `json:"outer_context_state,omitempty"`
	OutboundContextCause string `json:"outbound_context_cause,omitempty"`
	ProviderAttempt      int    `json:"provider_attempt,omitempty"`
	WireAttempt          int    `json:"wire_attempt,omitempty"`
}

type Explanation struct {
	Version        string                       `json:"version"`
	Completeness   Completeness                 `json:"completeness"`
	LogID          int64                        `json:"log_id"`
	Time           int64                        `json:"time"`
	RequestedModel string                       `json:"requested_model,omitempty"`
	Success        bool                         `json:"success"`
	FinalRoute     *RouteSummary                `json:"final_route,omitempty"`
	Decisions      []model.RoutingDecisionEvent `json:"decisions"`
	Attempts       []AttemptSummary             `json:"attempts"`
}

// BuildExplanation derives an immutable explanation exclusively from fields
// already persisted in RelayLog.Attempts. It intentionally does not inspect
// current routing state, credentials, request/response bodies, free-form attempt
// messages, or free-form decision detail, so a historical explanation cannot
// mutate or influence the live routing data plane and cannot expose payload text.
func BuildExplanation(logItem model.RelayLog) Explanation {
	explanation := Explanation{
		Version:        ExplanationVersion,
		LogID:          logItem.ID,
		Time:           logItem.Time,
		RequestedModel: logItem.RequestModelName,
		Success:        logItem.Success,
		Decisions:      make([]model.RoutingDecisionEvent, 0),
		Attempts:       make([]AttemptSummary, 0, len(logItem.Attempts)),
	}

	traced := 0
	realAttempts := 0
	decisionOnly := 0

	for _, attempt := range logItem.Attempts {
		if attempt.DecisionTraceVersion == model.RoutingDecisionTraceVersion {
			traced++
		}
		if attempt.AttemptKind == "decision_only" {
			decisionOnly++
		} else {
			realAttempts++
		}

		for _, event := range attempt.DecisionEvents {
			explanation.Decisions = append(explanation.Decisions, sanitizeDecisionEvent(event))
		}

		if attempt.AttemptKind == "decision_only" {
			continue
		}
		explanation.Attempts = append(explanation.Attempts, summarizeAttempt(attempt))
	}

	sort.SliceStable(explanation.Decisions, func(i, j int) bool {
		return explanation.Decisions[i].Sequence < explanation.Decisions[j].Sequence
	})
	sort.SliceStable(explanation.Attempts, func(i, j int) bool {
		return explanation.Attempts[i].AttemptNum < explanation.Attempts[j].AttemptNum
	})

	switch {
	case len(logItem.Attempts) > 0 && realAttempts == 0 && decisionOnly == len(logItem.Attempts):
		explanation.Completeness = CompletenessDecisionOnly
	case traced == 0:
		explanation.Completeness = CompletenessLegacyPartial
	case traced == len(logItem.Attempts):
		explanation.Completeness = CompletenessComplete
	default:
		explanation.Completeness = CompletenessMixedPartial
	}

	explanation.FinalRoute = selectFinalRoute(logItem.Attempts)
	return explanation
}

func sanitizeDecisionEvent(event model.RoutingDecisionEvent) model.RoutingDecisionEvent {
	// Detail is deliberately excluded from the dedicated routing-explanation
	// contract. It may originate from upstream free-form error text in older
	// persisted traces; typed reason/identity/protocol/expiry are sufficient for
	// operator diagnostics without exposing payload-like text.
	event.Detail = ""
	return event
}

func summarizeRoute(attempt model.ChannelAttempt) RouteSummary {
	protocol := attempt.SelectedProtocol
	if protocol == "" {
		protocol = attempt.ProtocolMode
	}
	return RouteSummary{
		AttemptNum:   attempt.AttemptNum,
		ChannelID:    attempt.ChannelID,
		ChannelKeyID: attempt.ChannelKeyID,
		ChannelName:  attempt.ChannelName,
		ModelName:    attempt.ModelName,
		Status:       attempt.Status,
		Duration:     attempt.Duration,
		Protocol:     protocol,
	}
}

func summarizeAttempt(attempt model.ChannelAttempt) AttemptSummary {
	trace := attempt.AttemptRoutingTrace
	return AttemptSummary{
		RouteSummary:         summarizeRoute(attempt),
		FailureDomain:        trace.FailureDomain,
		FailureScope:         trace.FailureScope,
		RuleID:               trace.RuleID,
		RetryDirective:       trace.RetryDirective,
		FailoverStopReason:   trace.FailoverStopReason,
		RuntimeEffect:        trace.RuntimeEffect,
		RuntimeState:         trace.RuntimeState,
		CooldownUntil:        trace.CooldownUntil,
		CircuitEffect:        trace.CircuitEffect,
		OutlierEffect:        trace.OutlierEffect,
		ReplaySafety:         trace.ReplaySafety,
		DispatchState:        trace.DispatchState,
		DownstreamCommitted:  trace.DownstreamCommitted,
		OuterContextState:    trace.OuterContextState,
		OutboundContextCause: trace.OutboundContextCause,
		ProviderAttempt:      trace.ProviderAttempt,
		WireAttempt:          trace.WireAttempt,
	}
}

func selectFinalRoute(attempts []model.ChannelAttempt) *RouteSummary {
	// A successful wire attempt is authoritative even if later bookkeeping
	// envelopes exist. Walk backwards because a request can contain retries.
	for i := len(attempts) - 1; i >= 0; i-- {
		attempt := attempts[i]
		if attempt.AttemptKind == "decision_only" {
			continue
		}
		if attempt.Status == model.AttemptSuccess {
			route := summarizeRoute(attempt)
			return &route
		}
	}
	// On failure, expose the last real failed dispatch. Skipped/circuit/capacity
	// records explain eligibility but are not themselves a final upstream route.
	for i := len(attempts) - 1; i >= 0; i-- {
		attempt := attempts[i]
		if attempt.AttemptKind == "decision_only" {
			continue
		}
		if attempt.Status == model.AttemptFailed {
			route := summarizeRoute(attempt)
			return &route
		}
	}
	return nil
}
