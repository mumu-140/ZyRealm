package relay

func isExplicitContentPolicyFailure(result attemptResult) bool {
	return containsAny(outlierErrorText(result.Err, result.UpstreamErrorBody), contentPolicyMarkers)
}
