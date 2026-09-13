package relay

// shouldFailoverModelCapacity keeps the single-provider path conservative: a
// model/capacity failure leaves the current provider immediately only when the
// request still has another provider candidate to try. With no alternative,
// the existing bounded same-provider retry behavior is preserved.
func shouldFailoverModelCapacity(request *relayRequest, channelID int, result attemptResult) bool {
	if result.Decision.Valid {
		return result.Decision.Domain == failureDomainModelCapacity && result.Decision.SkipProvider
	}
	if classifyRoutingFailure(result) != failureDomainModelCapacity {
		return false
	}
	return request != nil && request.iter != nil && request.iter.HasAlternativeProvider(channelID)
}
