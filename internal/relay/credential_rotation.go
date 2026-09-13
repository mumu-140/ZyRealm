package relay

import (
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
)

const defaultMaxCredentialsPerProvider = 2

func recordCredentialRoutingFailureRevision(channelID, keyID, revision int, result attemptResult, now time.Time) {
	text := outlierErrorText(result.Err, result.UpstreamErrorBody)
	if containsAny(text, credentialConcurrencyMarkers) {
		availability.RecordCredentialTransientFailureRevision(channelID, keyID, revision, "credential_concurrency", now)
		return
	}
	availability.RecordCredentialFailureRevision(channelID, keyID, revision, "credential_failure", now)
}

func recordCredentialRoutingFailure(channelID, keyID int, result attemptResult, now time.Time) {
	recordCredentialRoutingFailureRevision(channelID, keyID, 1, result, now)
}
