package relay

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
)

// recordRuntimeAvailabilityEvidence updates the shared fast-path runtime facts
// from one real upstream attempt. It is independent of GroupMode so changing
// routing strategies does not reset or fork provider/model availability state.
func recordRuntimeAvailabilityEvidence(
	ctx context.Context,
	channelID int,
	upstreamModel string,
	result attemptResult,
	now time.Time,
) {
	if result.Success {
		availability.RecordSuccess(channelID, upstreamModel, now)
		return
	}
	if result.Canceled || isRelayAttemptBudgetExceeded(result.Err) {
		return
	}
	if isAmbiguousTransportCancellation(ctx, result.Err) {
		availability.RecordModelSuspect(channelID, upstreamModel, "ambiguous_transport_cancel", now)
		return
	}
	if result.FirstTokenTimeout {
		availability.RecordModelFailure(channelID, upstreamModel, "first_token_timeout", now)
		return
	}
	if classifyRoutingFailure(result) == failureDomainModelCapacity {
		availability.EnsureModelFailure(channelID, upstreamModel, "model_capacity", now)
		return
	}
	if shouldFailoverProviderImmediately(result) {
		availability.RecordProviderFailure(channelID, "provider_transient", now)
	}
}
