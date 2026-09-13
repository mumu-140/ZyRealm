package relay

func isExplicitContentPolicyFailure(result attemptResult) bool {
	if result.Decision.Valid {
		return result.Decision.ContentPolicy
	}
	return containsAny(outlierErrorText(result.Err, result.UpstreamErrorBody), contentPolicyMarkers)
}
