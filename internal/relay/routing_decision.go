package relay

import (
	"context"
	"errors"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
)

type routingDirective string

const (
	routingDirectiveComplete            routingDirective = "complete"
	routingDirectiveTerminal            routingDirective = "terminal"
	routingDirectiveRetrySameCredential routingDirective = "retry_same_credential"
	routingDirectiveRotateCredential    routingDirective = "rotate_credential"
	routingDirectiveNextProvider        routingDirective = "next_provider"
	routingDirectiveProtocolOrProvider  routingDirective = "protocol_or_provider"
	routingDirectiveNextCandidate       routingDirective = "next_candidate"
)

type routingRuntimeEffect string

const (
	routingRuntimeNone               routingRuntimeEffect = "none"
	routingRuntimeSuccessClear       routingRuntimeEffect = "success_clear"
	routingRuntimeModelSuspect       routingRuntimeEffect = "model_suspect"
	routingRuntimeModelCooldown      routingRuntimeEffect = "model_cooldown"
	routingRuntimeProviderCooldown   routingRuntimeEffect = "provider_cooldown"
	routingRuntimeCredentialCooldown routingRuntimeEffect = "credential_cooldown"
)

type routingReplaySafety string

const (
	routingReplaySafe           routingReplaySafety = "safe"
	routingReplayNotSent        routingReplaySafety = "not_sent"
	routingReplayUnknownOutcome routingReplaySafety = "unknown_upstream_outcome"
	routingReplayCommitted      routingReplaySafety = "downstream_committed"
	routingReplayClientCanceled routingReplaySafety = "client_canceled"
)

type routingFailureScope string

const (
	routingScopeNone          routingFailureScope = "none"
	routingScopeRequest       routingFailureScope = "request"
	routingScopeCredential    routingFailureScope = "credential"
	routingScopeProviderModel routingFailureScope = "provider_model"
	routingScopeProvider      routingFailureScope = "provider"
)

// RoutingDecision is the single policy result consumed by retry/failover,
// runtime availability, outlier health, circuit gating, and attempt tracing.
// Marker/status parsing remains behind the compatibility classifiers, but one
// wire result is converted into this object exactly once on the relay path.
type RoutingDecision struct {
	Valid               bool
	Domain              routingFailureDomain
	RuleID              string
	FailureScope        routingFailureScope
	Directive           routingDirective
	RuntimeEffect       routingRuntimeEffect
	OutlierScope        failureScope
	CircuitEffect       string
	ReplaySafety        routingReplaySafety
	SkipProvider        bool
	RetrySameCredential bool
	Terminal            bool
	ContentPolicy       bool
}

func withRoutingDecision(ctx context.Context, request *relayRequest, channelID int, result attemptResult) attemptResult {
	if !result.Decision.Valid {
		result.Decision = decideRoutingAttempt(ctx, request, channelID, result)
	}
	return result
}

func decideRoutingAttempt(ctx context.Context, request *relayRequest, channelID int, result attemptResult) RoutingDecision {
	if result.Decision.Valid {
		return result.Decision
	}

	status := fallbackStatus(result)
	text := outlierErrorText(result.Err, result.UpstreamErrorBody)
	domain := classifyRoutingFailure(result)
	legacyScope := classifyFailureScope(status, text)
	hasAlternative := request != nil && request.iter != nil && request.iter.HasAlternativeProvider(channelID)

	decision := RoutingDecision{
		Valid:         true,
		Domain:        domain,
		RuleID:        routingRuleID(domain, status, text),
		FailureScope:  routingScopeFor(domain, legacyScope),
		Directive:     routingDirectiveNextCandidate,
		RuntimeEffect: routingRuntimeNone,
		OutlierScope:  legacyScope,
		CircuitEffect: "record_failure",
		ReplaySafety:  routingReplaySafetyFor(result),
	}

	if result.Success {
		decision.Domain = failureDomainUnknown
		decision.RuleID = "success"
		decision.FailureScope = routingScopeNone
		decision.Directive = routingDirectiveComplete
		decision.RuntimeEffect = routingRuntimeSuccessClear
		decision.OutlierScope = scopeIgnore
		decision.CircuitEffect = "success"
		decision.ReplaySafety = routingReplaySafe
		decision.Terminal = true
		return decision
	}
	if isManualInterrupt(ctx, result.Err) {
		decision.Domain = failureDomainUnknown
		decision.RuleID = "manual_interrupt"
		decision.FailureScope = routingScopeRequest
		decision.Directive = routingDirectiveTerminal
		decision.RuntimeEffect = routingRuntimeNone
		decision.OutlierScope = scopeIgnore
		decision.CircuitEffect = "none"
		decision.SkipProvider = false
		decision.RetrySameCredential = false
		decision.Terminal = true
		switch {
		case result.Written || result.ResetConversation:
			decision.ReplaySafety = routingReplayCommitted
		case result.DispatchState == dispatchMaybeSent:
			decision.ReplaySafety = routingReplayUnknownOutcome
		default:
			decision.ReplaySafety = routingReplayNotSent
		}
		return decision
	}
	if result.Canceled {
		decision.Domain = failureDomainUnknown
		decision.RuleID = "client_cancel"
		decision.FailureScope = routingScopeNone
		decision.Directive = routingDirectiveTerminal
		decision.OutlierScope = scopeIgnore
		decision.CircuitEffect = "none"
		decision.ReplaySafety = routingReplayClientCanceled
		decision.Terminal = true
		return decision
	}
	if isRelayAttemptBudgetExceeded(result.Err) {
		decision.Domain = failureDomainUnknown
		decision.RuleID = "attempt_budget"
		decision.FailureScope = routingScopeNone
		decision.Directive = routingDirectiveTerminal
		decision.OutlierScope = scopeIgnore
		decision.CircuitEffect = "none"
		decision.Terminal = true
		decision.SkipProvider = isProviderAttemptBudgetExceeded(result.Err)
		return decision
	}

	if isAmbiguousTransportCancellation(ctx, result.Err) {
		decision.Domain = failureDomainUnknown
		decision.RuleID = "ambiguous_transport_cancel"
		decision.FailureScope = routingScopeProviderModel
		decision.RuntimeEffect = routingRuntimeModelSuspect
		decision.OutlierScope = scopeIgnore
		decision.CircuitEffect = "none"
		decision.SkipProvider = true
		if result.Written || result.ResetConversation {
			decision.Directive = routingDirectiveTerminal
			decision.Terminal = true
			decision.ReplaySafety = routingReplayCommitted
		} else {
			decision.Directive = routingDirectiveNextProvider
			if result.DispatchState == dispatchMaybeSent {
				decision.ReplaySafety = routingReplayUnknownOutcome
			} else {
				decision.ReplaySafety = routingReplayNotSent
			}
		}
		return decision
	}

	if result.FirstTokenTimeout {
		decision.Domain = failureDomainUnknown
		decision.RuleID = "first_token_timeout"
		decision.FailureScope = routingScopeProviderModel
		decision.Directive = routingDirectiveNextProvider
		decision.RuntimeEffect = routingRuntimeModelCooldown
		decision.OutlierScope = scopeModel
		decision.CircuitEffect = "record_failure"
		decision.SkipProvider = true
		if result.Written || result.ResetConversation {
			decision.Directive = routingDirectiveTerminal
			decision.Terminal = true
			decision.ReplaySafety = routingReplayCommitted
			decision.CircuitEffect = "none"
		} else if result.DispatchState == dispatchMaybeSent {
			decision.ReplaySafety = routingReplayUnknownOutcome
		} else {
			decision.ReplaySafety = routingReplayNotSent
		}
		return decision
	}

	if result.Written || result.ResetConversation {
		decision.Domain = failureDomainUnknown
		decision.RuleID = "downstream_committed"
		if result.ResetConversation {
			decision.RuleID = "conversation_reset"
		}
		decision.Directive = routingDirectiveTerminal
		decision.RuntimeEffect = routingRuntimeNone
		decision.CircuitEffect = "none"
		decision.ReplaySafety = routingReplayCommitted
		decision.Terminal = true
		return decision
	}

	switch domain {
	case failureDomainCredential:
		decision.FailureScope = routingScopeCredential
		decision.Directive = routingDirectiveRotateCredential
		decision.RuntimeEffect = routingRuntimeCredentialCooldown
		decision.OutlierScope = scopeIgnore
		decision.CircuitEffect = "none"
	case failureDomainModelCapability:
		decision.FailureScope = routingScopeProviderModel
		decision.Directive = routingDirectiveProtocolOrProvider
		decision.OutlierScope = scopeIgnore
		decision.CircuitEffect = "none"
	case failureDomainModelCapacity:
		decision.FailureScope = routingScopeProviderModel
		decision.RuntimeEffect = routingRuntimeModelCooldown
		decision.CircuitEffect = "rate_limit_policy"
		if hasAlternative {
			decision.Directive = routingDirectiveNextProvider
			decision.SkipProvider = true
		} else if isRetryableStatus(result.StatusCode) {
			decision.Directive = routingDirectiveRetrySameCredential
			decision.RetrySameCredential = true
		}
	case failureDomainProviderTransient:
		decision.FailureScope = routingScopeProvider
		decision.Directive = routingDirectiveNextProvider
		decision.RuntimeEffect = routingRuntimeProviderCooldown
		decision.CircuitEffect = "record_failure"
		decision.SkipProvider = true
	case failureDomainRequest:
		decision.FailureScope = routingScopeRequest
		decision.ContentPolicy = isExplicitContentPolicyFailure(result)
		if decision.ContentPolicy {
			decision.Directive = routingDirectiveTerminal
			decision.OutlierScope = scopeIgnore
			decision.CircuitEffect = "none"
			decision.Terminal = true
		} else if status >= 400 && status < 500 {
			decision.CircuitEffect = "none"
		}
	default:
		if isRetryableStatus(result.StatusCode) {
			decision.Directive = routingDirectiveRetrySameCredential
			decision.RetrySameCredential = true
		}
	}

	return decision
}

func routingRuleID(domain routingFailureDomain, status int, text string) string {
	switch domain {
	case failureDomainRequest:
		if containsAny(text, contentPolicyMarkers) {
			return "content_policy"
		}
		if isBlockedInvalidRequestError(text) || containsAny(text, clientErrorMarkers) {
			return "request_invalid"
		}
		return "status_4xx_request"
	case failureDomainCredential:
		if containsAny(text, credentialConcurrencyMarkers) {
			return "credential_concurrency"
		}
		return "credential_auth_quota"
	case failureDomainModelCapability:
		return "model_capability"
	case failureDomainModelCapacity:
		return "model_capacity"
	case failureDomainProviderTransient:
		if isInterceptPageResponse(text) {
			return "provider_intercept"
		}
		if containsAny(text, providerTransientMarkers) {
			return "provider_transport"
		}
		return "provider_status"
	default:
		if errors.Is(contextErrorFromText(text), context.Canceled) {
			return "context_cancellation"
		}
		if errors.Is(contextErrorFromText(text), context.DeadlineExceeded) {
			return "context_deadline"
		}
		if status >= 500 {
			return "unknown_5xx"
		}
		if status == 0 {
			return "transport_unknown"
		}
		return "unknown"
	}
}

func contextErrorFromText(text string) error {
	if containsAny(text, []string{"context canceled"}) {
		return context.Canceled
	}
	if containsAny(text, []string{"context deadline exceeded"}) {
		return context.DeadlineExceeded
	}
	return nil
}

func routingScopeFor(domain routingFailureDomain, legacy failureScope) routingFailureScope {
	switch domain {
	case failureDomainRequest:
		return routingScopeRequest
	case failureDomainCredential:
		return routingScopeCredential
	case failureDomainModelCapacity, failureDomainModelCapability:
		return routingScopeProviderModel
	case failureDomainProviderTransient:
		return routingScopeProvider
	}
	switch legacy {
	case scopeChannel:
		return routingScopeProvider
	case scopeModel:
		return routingScopeProviderModel
	default:
		return routingScopeNone
	}
}

func routingReplaySafetyFor(result attemptResult) routingReplaySafety {
	if result.Canceled {
		return routingReplayClientCanceled
	}
	if result.Written || result.ResetConversation {
		return routingReplayCommitted
	}
	if result.DispatchState == dispatchNotSent {
		return routingReplayNotSent
	}
	return routingReplaySafe
}

func routingDomainString(domain routingFailureDomain) string {
	switch domain {
	case failureDomainRequest:
		return "request"
	case failureDomainCredential:
		return "credential"
	case failureDomainModelCapacity:
		return "model_capacity"
	case failureDomainModelCapability:
		return "model_capability"
	case failureDomainProviderTransient:
		return "provider_transient"
	default:
		return "unknown"
	}
}

func dispatchStateString(state dispatchState) string {
	if state == dispatchMaybeSent {
		return "maybe_sent"
	}
	return "not_sent"
}

func routingTraceOuterContextState(ctx context.Context) string {
	if ctx == nil {
		return "none"
	}
	if err := ctx.Err(); err != nil {
		return err.Error()
	}
	return "active"
}

func routingAttemptTrace(
	ctx context.Context,
	result attemptResult,
	decision RoutingDecision,
	credentialRevision int64,
	providerAttempt int,
	wireAttempt int,
) dbmodel.AttemptRoutingTrace {
	trace := dbmodel.AttemptRoutingTrace{
		RuleID:             decision.RuleID,
		FailureDomain:      routingDomainString(decision.Domain),
		FailureScope:       string(decision.FailureScope),
		Directive:          string(decision.Directive),
		RuntimeEffect:      string(decision.RuntimeEffect),
		OutlierScope:       decision.OutlierScope.String(),
		CircuitEffect:      decision.CircuitEffect,
		ReplaySafety:       string(decision.ReplaySafety),
		DispatchState:      dispatchStateString(result.DispatchState),
		OuterContextState:  routingTraceOuterContextState(ctx),
		CredentialRevision: credentialRevision,
		ProviderAttempt:    providerAttempt,
		WireAttempt:        wireAttempt,
		SkipProvider:       decision.SkipProvider,
		Terminal:           decision.Terminal,
	}
	if result.Plan != nil {
		trace.UpstreamProtocol = string(result.Plan.UpstreamProtocol())
		trace.AttemptKind = string(result.Plan.AttemptKind())
		trace.ProtocolMode = string(result.Plan.GroupProtocolMode())
		trace.FallbackReason = result.Plan.FallbackReason()
	}
	return trace
}

func recordRoutingDecision(
	ctx context.Context,
	request *relayRequest,
	channel *dbmodel.Channel,
	key dbmodel.ChannelKey,
	result attemptResult,
) attemptResult {
	if channel == nil {
		return result
	}
	result = withRoutingDecision(ctx, request, channel.ID, result)
	if request != nil && request.attemptBudget != nil && result.traceSpan != nil {
		request.attemptBudget.bindTraceSpan(result.traceSpan)
	}
	return result
}

func routingDecisionExpires(now time.Time, delay time.Duration) time.Time {
	if delay <= 0 {
		return time.Time{}
	}
	return now.Add(delay)
}
