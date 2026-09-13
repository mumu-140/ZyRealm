package relay

import "strings"

type routingFailureDomain uint8

const (
	failureDomainUnknown routingFailureDomain = iota
	failureDomainRequest
	failureDomainCredential
	failureDomainModelCapacity
	failureDomainModelCapability
	failureDomainProviderTransient
)

var credentialFailureMarkers = []string{
	"invalid api key",
	"invalid_api_key",
	"api_key_invalid",
	"api key not valid",
	"invalid token",
	"token invalid",
	"api_key_disabled",
	"key disabled",
	"token disabled",
	"account_deactivated",
	"account disabled",
	"insufficient account balance",
	"insufficient_balance",
	"insufficient_user_quota",
	"insufficient_quota",
	"余额不足",
	"额度不足",
}

var modelCapabilityMarkers = []string{
	"model_not_support",
	"model not supported",
	"model is not supported",
	"is not supported",
	"effort_not_supported",
	"does not support reasoning effort",
	"unsupported parameter(s)",
	"unsupported parameter:",
}

var modelCapacityRoutingMarkers = []string{
	"model_not_found",
	"model not found",
	"model_not_available",
	"model unavailable",
	"no available channel for model",
	"no available channels after filtering",
	"no active accounts",
	"no available accounts",
	"earliest recover at",
	"get_channel_failed",
	"upstream load saturated",
	"load saturated",
	"负载已经达到上限",
	"负载已达上限",
	"上游负载已饱和",
	"group requests-per-minute",
	"requests-per-minute limit",
	"rpm limit",
	"rate limit exceeded",
	"rate_limit_exceeded",
	"too_many_concurrent_requests",
	"too many concurrent requests",
}

var credentialConcurrencyMarkers = []string{
	"concurrency limit exceeded for account",
	"account concurrency limit",
}

func classifyRoutingFailure(result attemptResult) routingFailureDomain {
	if result.Decision.Valid {
		return result.Decision.Domain
	}
	if result.Success || result.Canceled || result.Written || result.ResetConversation ||
		isRelayAttemptBudgetExceeded(result.Err) {
		return failureDomainUnknown
	}

	text := outlierErrorText(result.Err, result.UpstreamErrorBody)
	status := fallbackStatus(result)

	// Explicit blocked/content semantics terminate before provider/key health.
	if isBlockedInvalidRequestError(text) || containsAny(text, contentPolicyMarkers) {
		return failureDomainRequest
	}

	// Capability markers deliberately precede generic client-error markers.
	// Some real relays wrap a capability error in an auth/request-shaped HTTP
	// envelope, and strings such as "unsupported parameter" overlap both sets.
	if containsAny(text, modelCapabilityMarkers) {
		return failureDomainModelCapability
	}

	if containsAny(text, clientErrorMarkers) {
		return failureDomainRequest
	}

	// Account-specific concurrency can often be recovered by another key in the
	// same provider, so recognize it before the generic 429 model-capacity rule.
	if containsAny(text, credentialConcurrencyMarkers) {
		return failureDomainCredential
	}

	if containsAny(text, modelCapacityRoutingMarkers) ||
		isUpstreamRateLimitError(text) {
		return failureDomainModelCapacity
	}

	if containsAny(text, credentialFailureMarkers) || isUpstreamQuotaError(text) {
		return failureDomainCredential
	}

	if shouldFailoverProviderImmediately(result) {
		return failureDomainProviderTransient
	}

	// Status is fallback only after semantic markers.
	switch status {
	case 429:
		return failureDomainModelCapacity
	case 401, 402:
		return failureDomainCredential
	case 400, 403, 404, 405, 409, 422:
		return failureDomainRequest
	}
	if status >= 400 && status < 500 {
		return failureDomainRequest
	}

	// Do not turn an unknown HTTP 500 into a provider verdict. The production
	// corpus contains content/business errors wrapped in 500 responses.
	if strings.Contains(text, "context canceled") || strings.Contains(text, "context deadline exceeded") {
		return failureDomainUnknown
	}
	return failureDomainUnknown
}
