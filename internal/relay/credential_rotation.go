package relay

import (
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
)

const defaultMaxCredentialsPerProvider = 2

func recordCredentialRoutingFailure(channelID, keyID int, result attemptResult, now time.Time) {
	text := outlierErrorText(result.Err, result.UpstreamErrorBody)
	if containsAny(text, credentialConcurrencyMarkers) {
		availability.RecordCredentialTransientFailure(channelID, keyID, "credential_concurrency", now)
		return
	}
	availability.RecordCredentialFailure(channelID, keyID, "credential_failure", now)
}
