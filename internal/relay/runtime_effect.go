package relay

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
)

// recordRuntimeAvailabilityEvidence updates the shared fast-path runtime facts
// from one real upstream attempt. It is independent of GroupMode so changing
// routing strategies does not reset or fork provider/model/credential state.
func recordRuntimeAvailabilityEvidence(
	ctx context.Context,
	channelID int,
	keyID int,
	upstreamModel string,
	result attemptResult,
	domain routingFailureDomain,
	now time.Time,
) {
	if result.Success {
		availability.RecordSuccess(channelID, upstreamModel, now)
		availability.RecordCredentialSuccess(channelID, keyID, now)
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

	switch domain {
	case failureDomainCredential:
		text := outlierErrorText(result.Err, result.UpstreamErrorBody)
		if containsAny(text, credentialConcurrencyMarkers) {
			availability.RecordCredentialTransientFailure(channelID, keyID, "credential_concurrency", now)
		} else {
			availability.RecordCredentialFailure(channelID, keyID, "credential_failure", now)
		}
	case failureDomainModelCapacity:
		availability.RecordModelFailure(channelID, upstreamModel, "model_capacity", now)
	case failureDomainProviderTransient:
		availability.RecordProviderFailure(channelID, "provider_transient", now)
	}
}
