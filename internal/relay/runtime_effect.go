package relay

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
)

// recordRuntimeAvailabilityEvidence applies the runtime effect already chosen by
// the unified routing decision. Legacy/unit-test callers that construct a raw
// attemptResult still get one decision synthesized here.
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
	}

	switch decision.RuntimeEffect {
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
			string(decision.RuntimeEffect),
			runtimeStateString(int(info.State)),
			unixMillisOrZero(info.CooldownUntil),
		)
	}
}
