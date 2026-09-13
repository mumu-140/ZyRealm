package relay

import (
	"context"
	"fmt"
	"testing"
)

func TestShouldFailoverProviderImmediately(t *testing.T) {
	cases := []struct {
		name string
		in   attemptResult
		want bool
	}{
		{
			name: "generic 502 upstream failure",
			in: attemptResult{StatusCode: 502, UpstreamErrorBody: `{"error":{"message":"Upstream request failed"}}`},
			want: true,
		},
		{
			name: "generic 503 unavailable",
			in: attemptResult{StatusCode: 503, UpstreamErrorBody: `{"error":{"message":"Service temporarily unavailable"}}`},
			want: true,
		},
		{
			name: "cloudflare 522",
			in: attemptResult{StatusCode: 522, UpstreamErrorBody: `<!DOCTYPE html><title>Connection timed out</title><span>cloudflare</span>`},
			want: true,
		},
		{
			name: "cloudflare tunnel 530",
			in: attemptResult{StatusCode: 530, UpstreamErrorBody: `Cloudflare Tunnel error: unable to reach it`},
			want: true,
		},
		{
			name: "dns failure",
			in: attemptResult{Err: fmt.Errorf("dial tcp: lookup relay.example: no such host")},
			want: true,
		},
		{
			name: "tls handshake failure",
			in: attemptResult{Err: fmt.Errorf("net/http: TLS handshake timeout")},
			want: true,
		},
		{
			name: "proxy refused",
			in: attemptResult{Err: fmt.Errorf("proxyconnect tcp: dial tcp 127.0.0.1:38903: connection refused")},
			want: true,
		},
		{
			name: "provider html parse failure",
			in: attemptResult{StatusCode: 200, Err: fmt.Errorf("failed to unmarshal response: invalid character '<' looking for beginning of value")},
			want: true,
		},
		{
			name: "do request failed",
			in: attemptResult{StatusCode: 500, UpstreamErrorBody: `{"error":{"code":"do_request_failed","message":"upstream error: do request failed"}}`},
			want: true,
		},
		{
			name: "model not found 503 stays model scoped",
			in: attemptResult{StatusCode: 503, UpstreamErrorBody: `{"error":{"code":"model_not_found","message":"No available channel for model claude-opus-5-thinking"}}`},
			want: false,
		},
		{
			name: "no active accounts 500 stays pool scoped",
			in: attemptResult{StatusCode: 500, UpstreamErrorBody: `{"error":{"message":"no active accounts available: total=1 active=0 cooldown=1, earliest recover at 2026-09-13 10:00:00"}}`},
			want: false,
		},
		{
			name: "rate limited 429 stays capacity scoped",
			in: attemptResult{StatusCode: 429, UpstreamErrorBody: `{"error":{"message":"group requests-per-minute limit exceeded"}}`},
			want: false,
		},
		{
			name: "insufficient balance stays credential scoped",
			in: attemptResult{StatusCode: 402, UpstreamErrorBody: `{"error":{"message":"Insufficient account balance","type":"insufficient_balance"}}`},
			want: false,
		},
		{
			name: "content policy wrapped in 500 is not provider failure",
			in: attemptResult{StatusCode: 500, UpstreamErrorBody: `{"error":{"code":"sensitive_words_detected","message":"content policy violation"}}`},
			want: false,
		},
		{
			name: "misleading 401 invalid key envelope but unsupported model",
			in: attemptResult{StatusCode: 401, UpstreamErrorBody: `{"error":{"message":"[401]: Model claude-opus-5-low is not supported","type":"authentication_error","code":"invalid_api_key"}}`},
			want: false,
		},
		{
			name: "unsupported request parameter is not provider transient",
			in: attemptResult{StatusCode: 400, UpstreamErrorBody: `{"error":{"message":"Unsupported parameter(s): prompt_cache_key"}}`},
			want: false,
		},
		{
			name: "ambiguous cancellation remains separate",
			in: attemptResult{Err: fmt.Errorf("failed to send request: %w", context.Canceled)},
			want: false,
		},
		{
			name: "first token timeout remains separate",
			in: attemptResult{FirstTokenTimeout: true, Err: errFirstTokenTimeout},
			want: false,
		},
		{
			name: "committed response cannot cross provider",
			in: attemptResult{Written: true, StatusCode: 502, UpstreamErrorBody: `upstream request failed`},
			want: false,
		},
		{
			name: "generic unknown 500 is conservative",
			in: attemptResult{StatusCode: 500, UpstreamErrorBody: `{"error":{"message":"unknown internal error"}}`},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldFailoverProviderImmediately(tc.in); got != tc.want {
				t.Fatalf("shouldFailoverProviderImmediately() = %t, want %t", got, tc.want)
			}
		})
	}
}
