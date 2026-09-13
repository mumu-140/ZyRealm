package relay

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
)

// recordRuntimeAvailabilityEvidence converts one attempt result into shared
// short-lived runtime eligibility state. Request/content/capability semantics are
// intentionally neutral here; they may affect routing but must not condemn the
// provider itself.
func recordRuntimeAvailabilityEvidence(ctx context.Context, channelID int, upstreamModel string, result attemptResult, now time.Time) {
	if result.Success {
		availability.RecordSuccess(channelID, upstreamModel, now)
		return
	}
	if result.Canceled || isRelayAttemptBudgetExceeded(result.Err) {
		return
	}
	if isAmbiguousTransportCancellation(ctx, result.Err) {
		availability.RecordModelSuspect(channelID, upstreamModel, "ambiguous_cancel", now)
		return
	}
	if result.FirstTokenTimeout {
		availability.RecordModelFailure(channelID, upstreamModel, "first_token_timeout", now)
		return
	}
	if classifyRoutingFailure(result) == failureDomainModelCapacity {
		availability.EnsureModelFailureWithRetryAfter(channelID, upstreamModel, "model_capacity", now, result.RetryAfter)
		return
	}
	if shouldFailoverProviderImmediately(result) {
		availability.RecordProviderFailureWithRetryAfter(channelID, "provider_transient", now, result.RetryAfter)
	}
}
