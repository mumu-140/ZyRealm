package relay

import (
	"net/http"
	"testing"
)

func TestModelCapabilityBeatsMisleadingAuthEnvelopeForHealthScope(t *testing.T) {
	text := `{"error":{"message":"model low/medium/high/xhigh is not supported","type":"invalid_request_error","code":"invalid_api_key"}}`

	result := attemptResult{
		StatusCode:        http.StatusUnauthorized,
		UpstreamStatus:    http.StatusUnauthorized,
		UpstreamErrorBody: text,
	}
	if got := classifyRoutingFailure(result); got != failureDomainModelCapability {
		t.Fatalf("expected routing domain model capability, got %v", got)
	}

	if got := classifyFailureScope(http.StatusUnauthorized, text); got != scopeIgnore {
		t.Fatalf("expected model capability mismatch not to poison provider/model health, got scope %v", got)
	}
}
