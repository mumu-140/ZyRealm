package relay

import "context"

// attachSidepathRoutingTrace persists the already-computed RoutingDecision on a
// sidepath AttemptSpan. Images and Compact do not own the Core request attempt
// budget, so provider/wire attempt indices intentionally remain zero.
//
// This helper is observability-only: it does not classify routing outcomes,
// choose retries, apply runtime health, or mutate candidate selection.
func attachSidepathRoutingTrace(ctx context.Context, result attemptResult, credentialRevision int) {
	if result.traceSpan == nil || !result.Decision.Valid {
		return
	}
	result.traceSpan.SetRoutingTrace(routingAttemptTrace(
		ctx,
		result,
		result.Decision,
		credentialRevision,
		0,
		0,
	))
}
