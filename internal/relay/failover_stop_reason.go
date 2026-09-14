package relay

// failoverStopReason is intentionally bounded. These values explain a
// control-flow gate that stopped an otherwise observable retry/failover path;
// they are not a second routing-decision taxonomy.
type failoverStopReason string

const (
	failoverStopDownstreamCommitted   failoverStopReason = "downstream_committed"
	failoverStopClientCanceled        failoverStopReason = "client_canceled"
	failoverStopNoAlternative         failoverStopReason = "no_alternative"
	failoverStopUnknownReplayBudget   failoverStopReason = "unknown_replay_budget"
	failoverStopWireAttemptBudget     failoverStopReason = "wire_attempt_budget"
	failoverStopProviderAttemptBudget failoverStopReason = "provider_attempt_budget"
	failoverStopCandidateExhausted    failoverStopReason = "candidate_exhausted"
)

func markFailoverStop(result attemptResult, reason failoverStopReason) {
	if result.traceSpan == nil || reason == "" {
		return
	}
	result.traceSpan.SetFailoverStopReason(string(reason))
}
