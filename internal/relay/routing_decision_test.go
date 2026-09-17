package relay

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestRoutingDecisionProviderTransient(t *testing.T) {
	result := attemptResult{
		Err:        errors.New("upstream service temporarily unavailable"),
		StatusCode: http.StatusServiceUnavailable,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if decision.Domain != failureDomainProviderTransient {
		t.Fatalf("domain=%v, want provider transient", decision.Domain)
	}
	if decision.Directive != routingDirectiveNextProvider || !decision.SkipProvider {
		t.Fatalf("directive=%q skipProvider=%t, want next provider", decision.Directive, decision.SkipProvider)
	}
	if decision.RuntimeEffect != routingRuntimeProviderCooldown {
		t.Fatalf("runtime effect=%q, want provider cooldown", decision.RuntimeEffect)
	}
	if decision.OutlierScope != scopeChannel {
		t.Fatalf("outlier scope=%v, want channel", decision.OutlierScope)
	}
}

func TestRoutingDecisionReasoningCapabilityBeatsMisleadingAuthEnvelope(t *testing.T) {
	result := attemptResult{
		Err:               errors.New("channel failed"),
		StatusCode:        http.StatusUnauthorized,
		UpstreamErrorBody: `{"error":{"message":"reasoning effort xhigh is not supported","code":"invalid_api_key"}}`,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if decision.Domain != failureDomainModelCapability {
		t.Fatalf("domain=%v, want model capability", decision.Domain)
	}
	if decision.RuleID != "reasoning_effort_unsupported" {
		t.Fatalf("rule=%q, want reasoning_effort_unsupported", decision.RuleID)
	}
	if decision.Directive != routingDirectiveNextProvider || !decision.SkipProvider {
		t.Fatalf("directive=%q skipProvider=%t, want immediate next provider", decision.Directive, decision.SkipProvider)
	}
	if decision.OutlierScope != scopeIgnore || decision.CircuitEffect != "none" {
		t.Fatalf("capability must be health-neutral: outlier=%v circuit=%q", decision.OutlierScope, decision.CircuitEffect)
	}
}

func TestRoutingDecisionGenericCapabilityBeatsMisleadingAuthEnvelope(t *testing.T) {
	result := attemptResult{
		Err:               errors.New("channel failed"),
		StatusCode:        http.StatusUnauthorized,
		UpstreamErrorBody: `{"error":{"message":"unsupported parameter: prompt_cache_key","code":"invalid_api_key"}}`,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if decision.Domain != failureDomainModelCapability {
		t.Fatalf("domain=%v, want model capability", decision.Domain)
	}
	if decision.RuleID != "model_capability" {
		t.Fatalf("rule=%q, want model_capability", decision.RuleID)
	}
	if decision.Directive != routingDirectiveProtocolOrProvider || decision.SkipProvider {
		t.Fatalf("directive=%q skipProvider=%t, want protocol fallback before provider failover", decision.Directive, decision.SkipProvider)
	}
	if decision.OutlierScope != scopeIgnore || decision.CircuitEffect != "none" {
		t.Fatalf("capability must be health-neutral: outlier=%v circuit=%q", decision.OutlierScope, decision.CircuitEffect)
	}
}

func TestRoutingDecisionCredentialConcurrencyRotatesWithoutSameKeyRetry(t *testing.T) {
	result := attemptResult{
		Err:               errors.New("channel failed"),
		StatusCode:        http.StatusTooManyRequests,
		UpstreamErrorBody: `{"error":{"message":"account concurrency limit exceeded"}}`,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if decision.Domain != failureDomainCredential {
		t.Fatalf("domain=%v, want credential", decision.Domain)
	}
	if decision.Directive != routingDirectiveRotateCredential || decision.RetrySameCredential {
		t.Fatalf("directive=%q retrySame=%t, want immediate credential rotation", decision.Directive, decision.RetrySameCredential)
	}
	if decision.RuntimeEffect != routingRuntimeCredentialCooldown {
		t.Fatalf("runtime effect=%q, want credential cooldown", decision.RuntimeEffect)
	}
}

func TestRoutingDecisionSingleProviderCapacityKeepsBoundedSameKeyRetry(t *testing.T) {
	result := attemptResult{
		Err:               errors.New("channel failed"),
		StatusCode:        http.StatusTooManyRequests,
		UpstreamErrorBody: `{"error":{"message":"rate limit exceeded"}}`,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if decision.Domain != failureDomainModelCapacity {
		t.Fatalf("domain=%v, want model capacity", decision.Domain)
	}
	if decision.Directive != routingDirectiveRetrySameCredential || !decision.RetrySameCredential {
		t.Fatalf("directive=%q retrySame=%t, want bounded same credential retry", decision.Directive, decision.RetrySameCredential)
	}
}

func TestRoutingDecisionAmbiguousMaybeSentTracksReplayRisk(t *testing.T) {
	result := attemptResult{Err: context.Canceled, DispatchState: dispatchMaybeSent}
	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if decision.RuleID != "ambiguous_transport_cancel" {
		t.Fatalf("rule=%q, want ambiguous transport cancel", decision.RuleID)
	}
	if decision.ReplaySafety != routingReplayUnknownOutcome {
		t.Fatalf("replay safety=%q, want unknown outcome", decision.ReplaySafety)
	}
	if decision.RuntimeEffect != routingRuntimeModelSuspect || !decision.SkipProvider {
		t.Fatalf("runtime=%q skipProvider=%t", decision.RuntimeEffect, decision.SkipProvider)
	}
}

func TestRoutingDecisionFirstTokenTimeoutIsModelScoped(t *testing.T) {
	result := attemptResult{
		Err:               errors.New("first token timeout"),
		FirstTokenTimeout: true,
		DispatchState:     dispatchMaybeSent,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if decision.RuleID != "first_token_timeout" || decision.FailureScope != routingScopeProviderModel {
		t.Fatalf("rule=%q scope=%q", decision.RuleID, decision.FailureScope)
	}
	if decision.RuntimeEffect != routingRuntimeModelCooldown || decision.Directive != routingDirectiveNextProvider {
		t.Fatalf("runtime=%q directive=%q", decision.RuntimeEffect, decision.Directive)
	}
}

func TestRoutingDecisionContentPolicyIsTerminalAndHealthNeutral(t *testing.T) {
	result := attemptResult{
		Err:               errors.New("channel failed"),
		StatusCode:        http.StatusInternalServerError,
		UpstreamErrorBody: `{"error":{"type":"content_policy_violation"}}`,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if !decision.ContentPolicy || !decision.Terminal || decision.Directive != routingDirectiveTerminal {
		t.Fatalf("content policy decision=%+v", decision)
	}
	if decision.OutlierScope != scopeIgnore || decision.CircuitEffect != "none" || decision.RuntimeEffect != routingRuntimeNone {
		t.Fatalf("content policy must be health-neutral: %+v", decision)
	}
}
