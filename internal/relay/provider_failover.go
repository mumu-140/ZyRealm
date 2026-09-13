package relay

import "strings"

var providerTransientMarkers = []string{
	"upstream request failed",
	"upstream service temporarily unavailable",
	"service temporarily unavailable",
	"do_request_failed",
	"proxyconnect",
	"connection refused",
	"connection reset by peer",
	"broken pipe",
	"no such host",
	"tls handshake",
	"i/o timeout",
	"client.timeout exceeded",
	"use of closed network connection",
	"cloudflare tunnel",
	"unable to reach it",
	"invalid character '<' looking for beginning of value",
	"upstream stream ended without forwarding any payload",
}

var modelOrCapacityMarkers = []string{
	"model_not_found",
	"model not found",
	"model_not_support",
	"model not supported",
	"model is not supported",
	"is not supported",
	"does not exist or you do not have access",
	"no available channel",
	"no available channels after filtering",
	"no active accounts",
	"earliest recover at",
	"get_channel_failed",
	"负载已经达到上限",
	"负载已达上限",
	"上游负载已饱和",
	"rate limit",
	"rate_limit",
	"too_many_concurrent_requests",
	"concurrency limit",
	"requests-per-minute",
	"rpm limit",
}

var contentPolicyMarkers = []string{
	"sensitive_words_detected",
	"content_policy_violation",
	"content policy",
}

// shouldFailoverProviderImmediately recognizes only high-confidence provider
// failure domains. Semantic request/model/credential evidence wins over HTTP
// status so a misleading 401/500/503 envelope cannot turn a capability or
// account-pool problem into a provider-wide failover decision.
func shouldFailoverProviderImmediately(result attemptResult) bool {
	if result.Success || result.Canceled || result.Written || result.ResetConversation ||
		result.FirstTokenTimeout || isRelayAttemptBudgetExceeded(result.Err) {
		return false
	}

	text := outlierErrorText(result.Err, result.UpstreamErrorBody)
	if text == "" && fallbackStatus(result) == 0 {
		return false
	}

	// Cancellations with a live outer request context are handled by the
	// dedicated ambiguous-cancellation path. Without the root context here,
	// never upgrade a cancellation string into a hard provider verdict.
	if strings.Contains(text, "context canceled") || strings.Contains(text, "context deadline exceeded") {
		return false
	}

	// Marker-first exclusions. These failures may justify another provider, but
	// they belong to request/model/capability/credential scopes rather than a
	// hard provider transport failure and receive their own runtime policy.
	if isBlockedInvalidRequestError(text) || containsAny(text, clientErrorMarkers) ||
		containsAny(text, contentPolicyMarkers) {
		return false
	}
	if containsAny(text, modelOrCapacityMarkers) || containsAny(text, modelErrorMarkers) ||
		isUpstreamContextLimitError(text) || isUpstreamRateLimitError(text) ||
		needsConversationRestart(text) {
		return false
	}
	if isUpstreamQuotaError(text) || isNoAvailableAccountError(text) ||
		containsAny(text, channelErrorMarkers) {
		return false
	}

	if isInterceptPageResponse(text) || containsAny(text, providerTransientMarkers) {
		return true
	}

	// Status is deliberately only a fallback. Generic 500 is excluded because
	// the production corpus contains business/content errors wrapped in HTTP 500.
	switch fallbackStatus(result) {
	case 502, 503, 504, 520, 521, 522, 523, 524, 530:
		return true
	default:
		return false
	}
}
