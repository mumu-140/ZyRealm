package relay

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

const observedAnthropicMessageContentError = `{"error":{"message":"Invalid JSON data: Failed to deserialize the JSON body into the target type: messages[6]: data did not match any variant of untagged enum MessageContent at line 1 column 41225","type":"invalid_request_error","param":"","code":"json_parse_error"}}`

func TestAnthropicPayloadSchemaMismatchRoutesAsCapability(t *testing.T) {
	result := attemptResult{
		StatusCode:        http.StatusBadRequest,
		UpstreamStatus:    http.StatusBadRequest,
		UpstreamErrorBody: observedAnthropicMessageContentError,
		Err:               errors.New("upstream error: 400"),
		DispatchState:     dispatchMaybeSent,
	}
	text := outlierErrorText(result.Err, result.UpstreamErrorBody)
	if !isAnthropicPayloadSchemaMismatch(text) {
		t.Fatal("expected observed Anthropic MessageContent error to match schema incompatibility")
	}
	if got := classifyRoutingFailure(result); got != failureDomainModelCapability {
		t.Fatalf("failure domain = %v, want model capability", got)
	}
	if got := classifyFailureScope(http.StatusBadRequest, text); got != scopeIgnore {
		t.Fatalf("failure scope = %v, want ignore", got)
	}

	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if decision.Directive != routingDirectiveProtocolOrProvider {
		t.Fatalf("directive = %q, want %q", decision.Directive, routingDirectiveProtocolOrProvider)
	}
	if decision.Terminal {
		t.Fatal("schema incompatibility must remain failover-eligible before delivery")
	}
	if decision.OutlierScope != scopeIgnore || decision.RouteLearningEffect != routingRouteLearningNone {
		t.Fatalf("schema incompatibility must be health-neutral: %+v", decision)
	}
}

func TestAnthropicPayloadSchemaMismatchDoesNotCaptureGenericBadRequest(t *testing.T) {
	body := `{"error":{"message":"Invalid JSON data near temperature","type":"invalid_request_error","code":"json_parse_error"}}`
	result := attemptResult{
		StatusCode:        http.StatusBadRequest,
		UpstreamStatus:    http.StatusBadRequest,
		UpstreamErrorBody: body,
		Err:               errors.New("upstream error: 400"),
	}
	if isAnthropicPayloadSchemaMismatch(outlierErrorText(result.Err, result.UpstreamErrorBody)) {
		t.Fatal("generic JSON parse error must not be reclassified as provider capability")
	}
	if got := classifyRoutingFailure(result); got != failureDomainRequest {
		t.Fatalf("failure domain = %v, want request", got)
	}
}
