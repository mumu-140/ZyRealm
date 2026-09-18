package relay

import (
	"fmt"
	"net/http"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
)

// attempt 统一管理一次通道尝试的完整生命周期
func (ra *relayAttempt) attempt() attemptResult {
	span := ra.iter.StartAttempt(ra.channel.ID, ra.usedKey.ID, ra.channel.Name)
	if ra.plan != nil {
		span.SetProtocolDecision(
			string(ra.plan.GroupProtocolMode()),
			string(ra.plan.IngressProtocol()),
			string(ra.plan.UpstreamProtocol()),
			string(ra.plan.AttemptKind()),
			ra.plan.FallbackReason(),
		)
	}
	statusCode, fwdErr := ra.forward()
	ra.usedKey.StatusCode = statusCode
	ra.usedKey.LastUseTimeStamp = time.Now().Unix()

	if fwdErr == nil {
		return ra.finishSuccessfulAttempt(span, statusCode)
	}
	if isClientCancellation(ra.requestContext(), fwdErr) {
		return ra.finishCanceledAttempt(span, statusCode, fwdErr)
	}
	return ra.finishFailedAttempt(span, statusCode, fwdErr)
}

func (ra *relayAttempt) attachRoutingDecision(span *balancer.AttemptSpan, result attemptResult) attemptResult {
	result = withRoutingDecision(ra.requestContext(), ra.relayRequest, ra.channel.ID, result)
	result.traceSpan = span
	providerAttempt, wireAttempt := 0, 0
	if ra.attemptBudget != nil {
		providerAttempt = ra.attemptBudget.providerAttemptIndex(ra.channel.ID)
		wireAttempt = ra.attemptBudget.wireAttemptIndex()
	}
	span.SetRoutingTrace(routingAttemptTrace(
		ra.requestContext(), result, result.Decision, ra.usedKey.CredentialRevision,
		providerAttempt, wireAttempt,
	))
	if ra.attemptBudget != nil {
		ra.attemptBudget.bindTraceSpan(span)
	}
	switch result.Decision.ReplaySafety {
	case routingReplayCommitted:
		markFailoverStop(result, failoverStopDownstreamCommitted)
	case routingReplayClientCanceled:
		markFailoverStop(result, failoverStopClientCanceled)
	}
	if result.Decision.SkipProvider && !result.Decision.Terminal && ra.iter != nil &&
		!ra.iter.HasAlternativeProvider(ra.channel.ID) {
		markFailoverStop(result, failoverStopNoAlternative)
	}
	return result
}

func (ra *relayAttempt) finishSuccessfulAttempt(span *balancer.AttemptSpan, statusCode int) attemptResult {
	ra.collectResponse()
	ra.usedKey.TotalCost += ra.metrics.Stats.InputCost + ra.metrics.Stats.OutputCost
	op.ChannelKeyUpdate(ra.usedKey)
	result := ra.attachRoutingDecision(span, attemptResult{Success: true, StatusCode: statusCode, DispatchState: ra.dispatchState})
	span.End(dbmodel.AttemptSuccess, statusCode, "")
	op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
		WaitTime: span.Duration().Milliseconds(), RequestSuccess: 1,
	})
	balancer.SetSticky(ra.apiKeyID, ra.requestModel, ra.channel.ID, ra.usedKey.ID)
	return result
}

func (ra *relayAttempt) finishCanceledAttempt(span *balancer.AttemptSpan, statusCode int, fwdErr error) attemptResult {
	written := ra.deliveryStarted()
	if written {
		ra.collectResponse()
	}
	op.ChannelKeyUpdate(ra.usedKey)
	result := ra.attachRoutingDecision(span, attemptResult{
		Written: written, Canceled: true, Err: fwdErr, StatusCode: statusCode,
		UpstreamErrorBody: ra.upstreamErrorBody, UpstreamStatus: ra.upstreamStatusCode,
		UpstreamStarted: ra.upstreamStarted, DispatchState: ra.dispatchState,
	})
	span.End(dbmodel.AttemptFailed, statusCode, fwdErr.Error())
	return result
}

func (ra *relayAttempt) finishFailedAttempt(span *balancer.AttemptSpan, statusCode int, fwdErr error) attemptResult {
	op.ChannelKeyUpdate(ra.usedKey)
	written := ra.deliveryStarted()
	if written {
		ra.collectResponse()
	}
	firstTokenTimeout := isFirstTokenTimeout(nil, fwdErr)
	result := ra.attachRoutingDecision(span, attemptResult{
		Success:           false,
		Written:           written,
		ResetConversation: statusCode == http.StatusConflict && needsConversationRestart(relayErrorMessage(fwdErr)),
		FirstTokenTimeout: firstTokenTimeout,
		Err:               fmt.Errorf("channel %s failed: %w", ra.channel.Name, fwdErr),
		StatusCode:        statusCode,
		RetryAfter:        ra.retryAfter,
		UpstreamErrorBody: ra.upstreamErrorBody,
		UpstreamStatus:    ra.upstreamStatusCode,
		UpstreamStarted:   ra.upstreamStarted,
		DispatchState:     ra.dispatchState,
	})
	span.End(dbmodel.AttemptFailed, statusCode, fwdErr.Error())
	op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
		WaitTime:      span.Duration().Milliseconds(),
		RequestFailed: 1,
	})
	return result
}

func (ra *relayAttempt) deliveryStarted() bool {
	if ra.streamPayloadWritten.Load() {
		return true
	}
	if ra.streamWriter != nil {
		return ra.streamWriter.Written()
	}
	if ra.internalRequest != nil && ra.internalRequest.Stream != nil && *ra.internalRequest.Stream {
		return false
	}
	return ra.c != nil && ra.c.Writer.Written()
}
