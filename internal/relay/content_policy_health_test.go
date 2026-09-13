package relay

import (
	"net/http"
	"testing"
)

func TestExplicitContentPolicyErrorIsHealthNeutralEvenOnHTTP500(t *testing.T) {
	text := `{"error":{"message":"sensitive_words_detected","type":"content_policy_violation"}}`
	result := attemptResult{
		StatusCode:        http.StatusInternalServerError,
		UpstreamStatus:    http.StatusInternalServerError,
		UpstreamErrorBody: text,
	}

	if got := classifyRoutingFailure(result); got != failureDomainRequest {
		t.Fatalf("expected explicit content policy error to be request-scoped, got %v", got)
	}
	if got := classifyFailureScope(http.StatusInternalServerError, text); got != scopeIgnore {
		t.Fatalf("expected explicit content policy error not to poison provider/model health, got scope %v", got)
	}
}
