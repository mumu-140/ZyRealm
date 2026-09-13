package relay

import (
	"net/http"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
)

func TestCredentialRoutingFailureUsesShortCooldownForAccountConcurrency(t *testing.T) {
	availability.Reset()
	t.Cleanup(availability.Reset)

	now := time.Date(2026, time.September, 13, 1, 40, 0, 0, time.UTC)
	result := attemptResult{
		StatusCode:        http.StatusTooManyRequests,
		UpstreamStatus:    http.StatusTooManyRequests,
		UpstreamErrorBody: `{"error":{"message":"concurrency limit exceeded for account","type":"rate_limit"}}`,
	}

	recordCredentialRoutingFailure(41, 7, result, now)

	if availability.CredentialAvailable(41, 7, now) {
		t.Fatalf("expected account-concurrency key to enter cooldown immediately")
	}
	if !availability.CredentialAvailable(41, 7, now.Add(16*time.Second)) {
		t.Fatalf("expected first transient credential cooldown to expire after 15 seconds")
	}
}

func TestCredentialRoutingFailureKeepsInvalidKeyOnStrongCooldown(t *testing.T) {
	availability.Reset()
	t.Cleanup(availability.Reset)

	now := time.Date(2026, time.September, 13, 1, 40, 0, 0, time.UTC)
	result := attemptResult{
		StatusCode:        http.StatusUnauthorized,
		UpstreamStatus:    http.StatusUnauthorized,
		UpstreamErrorBody: `{"error":{"message":"Invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}`,
	}

	recordCredentialRoutingFailure(41, 8, result, now)

	if availability.CredentialAvailable(41, 8, now.Add(16*time.Second)) {
		t.Fatalf("expected invalid key to remain on the strong credential cooldown after 16 seconds")
	}
	if !availability.CredentialAvailable(41, 8, now.Add(5*time.Minute+time.Second)) {
		t.Fatalf("expected first strong credential cooldown to expire after 5 minutes")
	}
}
