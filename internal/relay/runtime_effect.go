package relay

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
)

// applyRuntimeAvailabilityEffect applies a runtime effect that has already been
// projected by AttemptCoordinator. It does not classify status codes, errors,
// or transport facts.
func applyRuntimeAvailabilityEffect(
	channelID int,
	upstreamModel string,
	result attemptResult,
	effects attemptEffectPlan,
	now time.Time,
) {
	decision := result.Decision

	switch effects.RuntimeEffect {
	case routingRuntimeSuccessClear:
		availability.RecordSuccess(channelID, upstreamModel, now)
	case routingRuntimeModelSuspect:
		availability.RecordModelSuspect(channelID, upstreamModel, "ambiguous_transport_cancel", now)
	case routingRuntimeModelCooldown:
		switch {
		case result.FirstTokenTimeout:
			availability.RecordModelFailure(channelID, upstreamModel, "first_token_timeout", now)
		case decision.RuleID == "committed_stream_failure":
			availability.RecordModelFailure(channelID, upstreamModel, "committed_stream_failure", now)
		default:
			availability.EnsureModelFailureWithRetryAfter(channelID, upstreamModel, "model_capacity", now, result.RetryAfter)
		}
	case routingRuntimeProviderCooldown:
		availability.RecordProviderFailureWithRetryAfter(channelID, "provider_transient", now, result.RetryAfter)
	default:
		return
	}

	if result.traceSpan != nil {
		info := availability.CandidateInfo(channelID, upstreamModel, now)
		result.traceSpan.SetRoutingRuntime(
			string(effects.RuntimeEffect),
			runtimeStateString(int(info.State)),
			unixMillisOrZero(info.CooldownUntil),
		)
	}
}

// recordRuntimeAvailabilityEvidence is the compatibility path for callers that
// have not migrated to AttemptCoordinator yet. Raw/unit-test results may still
// synthesize one RoutingDecision here; Core HTTP and WS use
// applyRuntimeAvailabilityEffect directly from the coordinator projection.
func recordRuntimeAvailabilityEvidence(
	ctx context.Context,
	channelID int,
	upstreamModel string,
	result attemptResult,
	now time.Time,
) {
	decision := result.Decision
	if !decision.Valid {
		decision = decideRoutingAttempt(ctx, nil, channelID, result)
		result.Decision = decision
	}

	applyRuntimeAvailabilityEffect(
		channelID,
		upstreamModel,
		result,
		attemptEffectPlan{RuntimeEffect: decision.RuntimeEffect},
		now,
	)
}
