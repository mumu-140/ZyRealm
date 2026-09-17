package relay

import (
	"context"
	"testing"
)

func TestModelNotPricedSkipsCurrentProviderImmediately(t *testing.T) {
	decision := decideRoutingAttempt(context.Background(), nil, 7, attemptResult{
		StatusCode:        400,
		UpstreamErrorBody: `{"error":{"message":"model is not priced"}}`,
	})
	if decision.Directive != routingDirectiveNextProvider || !decision.SkipProvider {
		t.Fatalf("model not priced directive=%q skip_provider=%v, want next_provider/true", decision.Directive, decision.SkipProvider)
	}
	if decision.RetrySameCredential {
		t.Fatal("model not priced must not retry the same credential")
	}
}

func TestUnsupportedReasoningEffortSkipsCurrentProviderImmediately(t *testing.T) {
	decision := decideRoutingAttempt(context.Background(), nil, 7, attemptResult{
		StatusCode:        400,
		UpstreamErrorBody: `{"error":{"message":"reasoning effort xhigh is not supported","code":"effort_not_supported"}}`,
	})
	if decision.Directive != routingDirectiveNextProvider || !decision.SkipProvider {
		t.Fatalf("unsupported reasoning directive=%q skip_provider=%v, want next_provider/true", decision.Directive, decision.SkipProvider)
	}
}

func TestGenericModelCapabilityStillAllowsProtocolFallback(t *testing.T) {
	decision := decideRoutingAttempt(context.Background(), nil, 7, attemptResult{
		StatusCode:        400,
		UpstreamErrorBody: `{"error":{"message":"unsupported parameter: prompt_cache_key"}}`,
	})
	if decision.Directive != routingDirectiveProtocolOrProvider || decision.SkipProvider {
		t.Fatalf("generic capability directive=%q skip_provider=%v, want protocol_or_provider/false", decision.Directive, decision.SkipProvider)
	}
}
