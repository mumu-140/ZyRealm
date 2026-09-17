package relay

import (
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
)

const defaultMaxCredentialsPerProvider = 3

func recordCredentialRoutingFailureRevision(channelID, keyID, revision int, result attemptResult, now time.Time) {
	text := outlierErrorText(result.Err, result.UpstreamErrorBody)
	var cooldownUntil time.Time
	if containsAny(text, credentialConcurrencyMarkers) {
		cooldownUntil = availability.RecordCredentialTransientFailureRevision(channelID, keyID, revision, "credential_concurrency", now)
	} else {
		cooldownUntil = availability.RecordCredentialFailureRevision(channelID, keyID, revision, "credential_failure", now)
	}
	if result.traceSpan != nil {
		result.traceSpan.SetRoutingRuntime(
			string(routingRuntimeCredentialCooldown),
			"cooldown",
			unixMillisOrZero(cooldownUntil),
		)
	}
}

func recordCredentialRoutingFailure(channelID, keyID int, result attemptResult, now time.Time) {
	recordCredentialRoutingFailureRevision(channelID, keyID, 1, result, now)
}
