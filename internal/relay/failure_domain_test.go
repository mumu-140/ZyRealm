package relay

import "testing"

func TestClassifyRoutingFailureMarkerFirst(t *testing.T) {
	cases := []struct {
		name string
		in   attemptResult
		want routingFailureDomain
	}{
		{
			name: "401 unsupported model beats invalid api key envelope",
			in: attemptResult{StatusCode: 401, UpstreamErrorBody: `{"error":{"message":"[401]: Model claude-opus-5-low is not supported","type":"authentication_error","code":"invalid_api_key"}}`},
			want: failureDomainModelCapability,
		},
		{
			name: "effort unsupported is capability",
			in: attemptResult{StatusCode: 400, UpstreamErrorBody: `{"error":{"message":"Model glm-5.3 does not support reasoning effort \"xhigh\"","code":"effort_not_supported"}}`},
			want: failureDomainModelCapability,
		},
		{
			name: "provider specific unsupported parameter is capability",
			in: attemptResult{StatusCode: 400, UpstreamErrorBody: `{"error":{"message":"Validation: Unsupported parameter(s): prompt_cache_key"}}`},
			want: failureDomainModelCapability,
		},
		{
			name: "503 model not found is model capacity",
			in: attemptResult{StatusCode: 503, UpstreamErrorBody: `{"error":{"code":"model_not_found","message":"No available channel for model claude-opus-5-thinking"}}`},
			want: failureDomainModelCapacity,
		},
		{
			name: "no active accounts is model capacity",
			in: attemptResult{StatusCode: 500, UpstreamErrorBody: `{"error":{"message":"no active accounts available: total=1 active=0 cooldown=1, earliest recover at 2026-09-13 10:00:00"}}`},
			want: failureDomainModelCapacity,
		},
		{
			name: "429 group rpm is model capacity",
			in: attemptResult{StatusCode: 429, UpstreamErrorBody: `{"error":{"message":"group requests-per-minute limit exceeded","type":"rate_limit_exceeded"}}`},
			want: failureDomainModelCapacity,
		},
		{
			name: "account concurrency is credential",
			in: attemptResult{StatusCode: 429, UpstreamErrorBody: `{"error":{"message":"Concurrency limit exceeded for account, please retry later"}}`},
			want: failureDomainCredential,
		},
		{
			name: "explicit invalid key is credential",
			in: attemptResult{StatusCode: 401, UpstreamErrorBody: `{"error":{"message":"Invalid API key"}}`},
			want: failureDomainCredential,
		},
		{
			name: "402 insufficient balance is credential",
			in: attemptResult{StatusCode: 402, UpstreamErrorBody: `{"error":{"message":"Insufficient account balance","type":"insufficient_balance"}}`},
			want: failureDomainCredential,
		},
		{
			name: "cloudflare html is provider transient",
			in: attemptResult{StatusCode: 403, UpstreamErrorBody: `<!DOCTYPE html><title>Attention Required!</title><span>cloudflare cf-ray</span>`},
			want: failureDomainProviderTransient,
		},
		{
			name: "generic upstream 502 is provider transient",
			in: attemptResult{StatusCode: 502, UpstreamErrorBody: `{"error":{"message":"Upstream request failed"}}`},
			want: failureDomainProviderTransient,
		},
		{
			name: "content policy wrapped in 500 is request",
			in: attemptResult{StatusCode: 500, UpstreamErrorBody: `{"error":{"code":"sensitive_words_detected","message":"content policy violation"}}`},
			want: failureDomainRequest,
		},
		{
			name: "generic unknown 500 stays unknown",
			in: attemptResult{StatusCode: 500, UpstreamErrorBody: `{"error":{"message":"unknown internal error"}}`},
			want: failureDomainUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRoutingFailure(tc.in); got != tc.want {
				t.Fatalf("classifyRoutingFailure() = %v, want %v", got, tc.want)
			}
		})
	}
}
