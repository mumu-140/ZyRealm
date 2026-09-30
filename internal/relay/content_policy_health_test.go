package relay

import (
	"net/http"
	"testing"
)

func TestSensitiveAndLeakProtectionFailuresAreModelScopedHealthFailures(t *testing.T) {
	tests := []struct {
		name string
		body string
		rule string
	}{
		{
			name: "structured sensitive words code",
			body: `{"error":{"message":"request blocked by leak protection (request id: example)","type":"new_api_error","param":"","code":"sensitive_words_detected"}}`,
			rule: "sensitive_words_detected",
		},
		{
			name: "message only leak protection",
			body: `{"error":{"message":"request blocked by leak protection (request id: example)","type":"new_api_error"}}`,
			rule: "leak_protection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := attemptResult{
				StatusCode:        http.StatusInternalServerError,
				UpstreamStatus:    http.StatusInternalServerError,
				UpstreamErrorBody: tt.body,
			}
			if got := classifyRoutingFailure(result); got != failureDomainRequest {
				t.Fatalf("domain=%v, want request", got)
			}
			if got := classifyFailureScope(http.StatusInternalServerError, tt.body); got != scopeModel {
				t.Fatalf("scope=%v, want model", got)
			}
			decision := decideRoutingAttempt(nil, nil, 1, result)
			if decision.RuleID != tt.rule {
				t.Fatalf("rule=%q, want %q", decision.RuleID, tt.rule)
			}
			if !decision.Terminal || decision.Directive != routingDirectiveTerminal {
				t.Fatalf("decision must remain terminal: %+v", decision)
			}
			if decision.FailureScope != routingScopeProviderModel || decision.OutlierScope != scopeModel {
				t.Fatalf("decision must count one provider-model failure: %+v", decision)
			}
			if decision.RuntimeEffect != routingRuntimeNone {
				t.Fatalf("failure must not trigger hard runtime cooldown: %+v", decision)
			}
		})
	}
}

func TestGenericContentPolicyViolationRemainsHealthNeutral(t *testing.T) {
	text := `{"error":{"message":"content policy violation","type":"content_policy_violation"}}`
	result := attemptResult{
		StatusCode:        http.StatusInternalServerError,
		UpstreamStatus:    http.StatusInternalServerError,
		UpstreamErrorBody: text,
	}

	if got := classifyRoutingFailure(result); got != failureDomainRequest {
		t.Fatalf("domain=%v, want request", got)
	}
	if got := classifyFailureScope(http.StatusInternalServerError, text); got != scopeIgnore {
		t.Fatalf("scope=%v, want ignore", got)
	}
}
