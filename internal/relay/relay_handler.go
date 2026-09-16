package relay

import (
	"fmt"
	"net/http"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/outlierwindow"
	"github.com/bestruirui/octopus/internal/protocol"
	"github.com/bestruirui/octopus/internal/protocolroute"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/relay/compress"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

const manualInterruptHTTPStatus = 499

func Handler(inboundType inbound.InboundType, c *gin.Context) {
	handler, ok := newRelayHandler(inboundType, c)
	if !ok {
		return
	}
	defer handler.heartbeat.Stop()
	if handler.request != nil && handler.request.control != nil {
		requestID := handler.request.control.Snapshot().RequestID
		registerLiveRequest(handler.request.control)
		defer unregisterLiveRequest(requestID)
	}
	handler.run()
}

type relayHandler struct {
	inboundType            inbound.InboundType
	c                      *gin.Context
	group                  dbmodel.Group
	protocolRoutingEnabled bool
	replayState            *wsConversationState
	iterator               *balancer.Iterator
	heartbeat              *earlyHeartbeat
	metrics                *RelayMetrics
	request                *relayRequest
	passthroughRequired    bool
	passthroughCapable     bool
	lastErr                error
	lastResult             attemptResult
	capacitySkipped        bool
	rateSkipped            bool
	maxRetries             int
}

func newRelayHandler(inboundType inbound.InboundType, c *gin.Context) (*relayHandler, bool) {
	rawBody, request, inAdapter, err := parseRequest(inboundType, c)
	if err != nil || !validateSupportedModel(c, request.Model) {
		return nil, false
	}
	group, err := op.GroupGetEnabledMap(request.Model, c.Request.Context())
	if err != nil {
		resp.ErrorWithCode(c, http.StatusNotFound, CodeRelayModelNotFound, "model not found")
		return nil, false
	}
	// 请求压缩挂点: 分组配置 + 全局 master 均开启时生效; fail-open, 不影响转发
	compress.MaybeApply(request, group)
	request, replayState := prepareHTTPReplay(inboundType, c.GetInt("api_key_id"), group.ID, request.Model, request)
	iterator := newRelayIterator(group, c.GetInt("api_key_id"), request.Model, replayState)
	if iterator.Len() == 0 {
		if writeRuntimeCooldownUnavailable(c, group.Items, request.Model) {
			return nil, false
		}
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return nil, false
	}
	return buildRelayHandler(inboundType, c, group, op.ProtocolRoutingEnabled(),
		rawBody, request, inAdapter, replayState, iterator), true
}

func newRelayIterator(group dbmodel.Group, apiKeyID int, requestModel string, replayState *wsConversationState) *balancer.Iterator {
	var sticky *balancer.SessionEntry
	if replayState != nil {
		sticky = responsesReplayStateToSticky(replayState)
		if sticky != nil {
			log.Debugf("HTTP replay sticky routing preference (channel=%d, key=%d)", sticky.ChannelID, sticky.ChannelKeyID)
		}
	}
	return balancer.NewIteratorWithPreference(group, apiKeyID, requestModel, sticky)
}

func buildRelayHandler(
	inboundType inbound.InboundType,
	c *gin.Context,
	group dbmodel.Group,
	protocolRoutingEnabled bool,
	rawBody []byte,
	request *model.InternalLLMRequest,
	inAdapter model.Inbound,
	replayState *wsConversationState,
	iterator *balancer.Iterator,
) *relayHandler {
	apiKeyID := c.GetInt("api_key_id")
	heartbeat := startEarlyHeartbeat(c, request.Stream != nil && *request.Stream)
	metrics := NewRelayMetrics(apiKeyID, request.Model, rawBody, request)
	if replayState != nil {
		metrics.SetWSMode(dbmodel.RelayLogWSModeReplay)
		metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryReplay)
	}
	control := newRelayControl(c.Request.Context(), LiveRequestSnapshot{
		StartedAt:       time.Now().UnixMilli(),
		Transport:       "http",
		APIKeyID:        apiKeyID,
		RequestedModel:  request.Model,
		IngressProtocol: string(protocol.FromAPIFormat(request.RawAPIFormat)),
		Phase:           string(livePhaseRouting),
	})
	requestContext := &relayRequest{
		c: c, control: control, inAdapter: inAdapter, internalRequest: request, metrics: metrics,
		apiKeyID: apiKeyID, requestModel: request.Model, groupID: group.ID,
		groupSessionTTL: group.SessionKeepTime, iter: iterator, rawBody: rawBody, heartbeat: heartbeat,
		attemptBudget: newRelayAttemptBudget(),
	}
	return &relayHandler{
		inboundType: inboundType, c: c, group: group,
		protocolRoutingEnabled: protocolRoutingEnabled, replayState: replayState,
		iterator: iterator, heartbeat: heartbeat, metrics: metrics, request: requestContext,
		passthroughRequired: request.HasOpenAIResponsesPassthrough(),
		maxRetries:          sameChannelRetryLimit(group.RetryEnabled, group.MaxRetries),
	}
}

func (h *relayHandler) run() {
	ctx := h.request.requestContext()
	for h.iterator.Next() {
		if h.request.attemptBudget != nil && h.request.attemptBudget.wireExhausted() {
			h.lastErr = errRelayWireAttemptsExceeded
			break
		}
		if err := contextError(ctx); err != nil {
			if isManualInterrupt(ctx, err) {
				log.Debugf("manual interrupt requested, stopping relay")
				h.metrics.SaveWithChannelStats(ctx, false, err, h.iterator.Attempts(), false)
				h.heartbeat.FlushOrError(h.c, manualInterruptHTTPStatus, "request interrupted")
				return
			}
			log.Debugf("request context canceled, stopping retry")
			h.markFailoverStop(failoverStopClientCanceled)
			h.metrics.SaveWithChannelStats(ctx, false, err, h.iterator.Attempts(), false)
			return
		}
		if h.processCandidate() {
			return
		}
	}
	h.markExhaustedFailoverStop()
	writeExhaustedRelayError(exhaustedRelayInput{
		c: h.c, heartbeat: h.heartbeat, metrics: h.metrics, attempts: h.iterator.Attempts(),
		lastErr: h.lastErr, lastResult: h.lastResult, capacitySkipped: h.capacitySkipped,
		rateSkipped: h.rateSkipped, passthroughRequired: h.passthroughRequired,
		passthroughCapableFound: h.passthroughCapable,
	})
}

func (h *relayHandler) markFailoverStop(reason failoverStopReason) {
	if h == nil || reason == "" {
		return
	}
	if h.lastResult.traceSpan != nil {
		markFailoverStop(h.lastResult, reason)
		return
	}
	if h.request != nil && h.request.attemptBudget != nil {
		h.request.attemptBudget.markStop(reason)
	}
}

func (h *relayHandler) markExhaustedFailoverStop() {
	if h == nil {
		return
	}
	reason := failoverStopCandidateExhausted
	if budgetReason, ok := attemptBudgetFailoverStopReason(h.lastErr); ok {
		reason = budgetReason
	}
	h.markFailoverStop(reason)
}

func (h *relayHandler) processCandidate() bool {
	ctx := h.request.requestContext()
	item := h.iterator.Item()
	channel, err := op.ChannelGet(item.ChannelID, ctx)
	if err != nil {
		log.Warnf("failed to get channel %d: %v", item.ChannelID, err)
		h.iterator.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
		h.lastErr = err
		return false
	}
	if !channel.Enabled {
		h.iterator.Skip(channel.ID, 0, channel.Name, "channel disabled")
		return false
	}
	if h.request.attemptBudget != nil && !h.request.attemptBudget.canUseProvider(channel.ID) {
		h.iterator.Skip(channel.ID, 0, channel.Name, errRelayProviderAttemptsExceeded.Error())
		h.iterator.SkipProvider(channel.ID)
		h.lastErr = errRelayProviderAttemptsExceeded
		return false
	}

	upstreamModel := balancer.ItemUpstreamModel(item, h.request.requestModel)
	legacyEligible, reason := legacyChannelEligibility(channel, h.request.internalRequest, h.passthroughRequired)

	runtimeLease, runtimeEligible := availability.AcquireCandidate(channel.ID, upstreamModel, time.Now())
	if !runtimeEligible {
		h.iterator.Skip(channel.ID, 0, channel.Name, "runtime cooldown or half-open lease busy")
		return false
	}
	defer func() {
		availability.ReleaseLease(runtimeLease, time.Now())
	}()

	excludedKeyIDs := make(map[int]struct{}, defaultMaxCredentialsPerProvider)
	for credentialAttempt := 0; credentialAttempt < defaultMaxCredentialsPerProvider; credentialAttempt++ {
		// Capacity is reserved before fair credential selection. A saturated
		// provider therefore cannot consume credential scheduling progress.
		if !h.reserveCandidateCapacity(channel) {
			return false
		}

		key, plans := h.selectCandidateAttempt(channel, upstreamModel, legacyEligible, excludedKeyIDs)
		if key.ChannelKey == "" || len(plans) == 0 {
			balancer.ReleaseChannel(channel.ID)
			if key.ChannelKey != "" {
				h.iterator.Skip(channel.ID, key.ID, channel.Name, reason)
			}
			if len(excludedKeyIDs) > 0 {
				h.iterator.SkipProvider(channel.ID)
			}
			return false
		}
		for _, plan := range plans {
			if plan.UpstreamProtocol() == protocol.OpenAIResponse {
				h.passthroughCapable = true
			}
		}

		if !h.consumeCandidateRPM(channel, key) {
			return false
		}
		result := runSameChannelAttempts(ctx, h.request, channel, key, plans,
			h.group.FirstTokenTimeOut, h.maxRetries)
		usedPlan := plans[0]
		if result.Plan != nil {
			usedPlan = result.Plan
		}

		if classifyRoutingFailure(result) == failureDomainCredential {
			recordCredentialRoutingFailureRevision(channel.ID, key.ID, key.CredentialRevision, result, time.Now())
			excludedKeyIDs[key.ID] = struct{}{}
			h.lastErr = result.Err
			h.lastResult = result
			if credentialAttempt+1 >= defaultMaxCredentialsPerProvider {
				h.iterator.SkipProvider(channel.ID)
				return false
			}
			continue
		}

		return h.handleAttemptResult(channel, key, usedPlan, result)
	}

	h.iterator.SkipProvider(channel.ID)
	return false
}

func (h *relayHandler) selectCandidateAttempt(channel *dbmodel.Channel, upstreamModel string, legacyEligible bool, excludeKeyIDs map[int]struct{}) (dbmodel.ChannelKey, []*protocolroute.AttemptPlan) {
	log.Debugf("request model %s, mode: %d, forwarding to channel: %s model: %s (attempt %d/%d, sticky=%t)",
		h.request.requestModel, h.group.Mode, channel.Name, upstreamModel,
		h.iterator.Index()+1, h.iterator.Len(), h.iterator.IsSticky())
	return selectChannelAttempt(channelAttemptInput{
		channel: channel, upstreamModel: upstreamModel, requestModel: h.request.requestModel,
		request: h.request.internalRequest, iterator: h.iterator, excludeKeyIDs: excludeKeyIDs, group: h.group,
		protocolRoutingEnabled: h.protocolRoutingEnabled, legacyEligible: legacyEligible,
		responsesPassthroughRequired: h.passthroughRequired,
	})
}

func (h *relayHandler) reserveCandidateCapacity(channel *dbmodel.Channel) bool {
	if balancer.TryAcquireChannel(channel.ID, channel.MaxConcurrency) {
		return true
	}
	h.capacitySkipped = true
	h.iterator.SkipCapacity(channel.ID, 0, channel.Name,
		fmt.Sprintf("channel at max concurrency (%d)", channel.MaxConcurrency))
	return false
}

func (h *relayHandler) consumeCandidateRPM(channel *dbmodel.Channel, key dbmodel.ChannelKey) bool {
	if balancer.TryConsumeChannelRPM(channel.ID, channel.MaxRPM, time.Now()) {
		return true
	}
	balancer.ReleaseChannel(channel.ID)
	h.rateSkipped = true
	h.iterator.SkipRateLimit(channel.ID, key.ID, channel.Name,
		fmt.Sprintf("channel at max rpm (%d)", channel.MaxRPM))
	return false
}

func (h *relayHandler) handleAttemptResult(channel *dbmodel.Channel, key dbmodel.ChannelKey, plan *protocolroute.AttemptPlan, result attemptResult) bool {
	ctx := h.request.requestContext()
	now := time.Now()
	result = withRoutingDecision(ctx, h.request, channel.ID, result)
	recordRuntimeAvailabilityEvidence(ctx, channel.ID, plan.UpstreamModel(), result, now)

	decision := result.Decision
	manualInterrupt := decision.RuleID == "manual_interrupt"
	ambiguousCancellation := decision.RuleID == "ambiguous_transport_cancel"
	budgetExceeded := isRelayAttemptBudgetExceeded(result.Err)
	failureDomain := decision.Domain
	explicitContentPolicy := decision.ContentPolicy

	if manualInterrupt {
		h.lastErr = result.Err
		h.lastResult = result
		h.metrics.SaveWithChannelStats(ctx, false, result.Err, h.iterator.Attempts(), false)
		if !result.Written && !h.c.Writer.Written() {
			h.heartbeat.FlushOrError(h.c, manualInterruptHTTPStatus, "request interrupted")
		}
		return true
	}

	// Any MAYBE_SENT failure whose upstream outcome is unknown spends the single
	// balanced cross-provider replay allowance before another provider is tried.
	// This includes ambiguous transport cancellation and first-token timeout: in
	// both cases the upstream may already be executing the request. NOT_SENT
	// failures remain free to fail over because duplicate execution is impossible.
	if decision.ReplaySafety == routingReplayUnknownOutcome && decision.SkipProvider &&
		h.iterator.HasAlternativeProvider(channel.ID) && !result.Written && !result.ResetConversation &&
		h.request.attemptBudget != nil {
		if !h.request.attemptBudget.tryUnknownCrossProviderReplay() {
			// Balanced replay policy: once an unknown upstream outcome has already
			// been replayed across providers, terminate rather than risk another
			// duplicate execution/billing event.
			h.metrics.SaveWithChannelStats(ctx, false, result.Err, h.iterator.Attempts(), false)
			h.heartbeat.FlushOrError(h.c, http.StatusBadGateway, "channel failed")
			return true
		}
	}

	if decision.SkipProvider || isProviderAttemptBudgetExceeded(result.Err) {
		// Request-local provider skipping is part of the unified decision. Shared
		// runtime health remains separately scoped by RuntimeEffect.
		h.iterator.SkipProvider(channel.ID)
	}

	// 健康度上报与熔断上报的口径不同：
	//   - 熔断只处理「可继续 failover」的失败（Written/ResetConversation 已终止本次请求）；
	//   - 健康度必须覆盖 Written 与 ResetConversation：上游流中断、要求重建会话都是上游故障证据，
	//     漏掉它们会让持续吐流失败的渠道-模型永远显示健康。
	//   - Canceled 是客户端主动断开，与上游健康无关，继续排除。
	//   - 单次 ambiguous transport cancellation 只做请求内绕开，暂不污染慢速 outlier/circuit；
	//     它由共享 runtime availability 记录为 SUSPECT 并在短期重复时升级。
	//   - request-local attempt budget exhaustion is not upstream health evidence.
	if !result.Success && !result.Canceled && !ambiguousCancellation && !budgetExceeded {
		reportOutlierFailure(channel.ID, plan.UpstreamModel(), result.StatusCode,
			outlierErrorText(result.Err, result.UpstreamErrorBody), now)
	}
	if !result.Success && !result.Written && !result.Canceled && !ambiguousCancellation && !budgetExceeded &&
		!result.ResetConversation && failureDomain != failureDomainModelCapability && !explicitContentPolicy {
		failureKind := circuitFailureKind(h.group.RetryEnabled, result.StatusCode)
		balancer.RecordFailure(channel.ID, key.ID, plan.UpstreamModel(), failureKind)
		if failureKind == balancer.FailureHard {
			maybeLearnManagedRoute(ctx, channel.ID, plan.UpstreamModel(), h.inboundType, result.Err)
		}
	}
	switch classifyAttemptResult(result) {
	case attemptActionSuccess:
		return h.handleSuccessfulAttempt(channel, key, plan, result)
	case attemptActionCanceled:
		h.metrics.SaveWithChannelStats(ctx, false, result.Err, h.iterator.Attempts(), false)
		return true
	case attemptActionResetConversation:
		h.metrics.SaveWithChannelStats(ctx, false, result.Err, h.iterator.Attempts(), false)
		if publicErr, ok := classifyWSPublicError(result.Err, result.StatusCode); ok {
			h.heartbeat.FlushOrError(h.c, publicErr.Status, publicErr.Message)
		} else {
			h.heartbeat.FlushOrError(h.c, result.StatusCode, result.Err.Error())
		}
		return true
	case attemptActionWritten:
		h.metrics.SaveWithChannelStats(ctx, false, result.Err, h.iterator.Attempts(), false)
		return true
	}
	if explicitContentPolicy {
		h.metrics.SaveWithChannelStats(ctx, false, result.Err, h.iterator.Attempts(), false)
		statusCode := result.StatusCode
		if statusCode <= 0 {
			statusCode = http.StatusBadRequest
		}
		h.heartbeat.FlushOrError(h.c, statusCode, "channel failed")
		return true
	}
	h.lastErr = result.Err
	h.lastResult = result
	return false
}

type attemptAction uint8

const (
	attemptActionContinue attemptAction = iota
	attemptActionSuccess
	attemptActionCanceled
	attemptActionResetConversation
	attemptActionWritten
)

func classifyAttemptResult(result attemptResult) attemptAction {
	if result.Success {
		return attemptActionSuccess
	}
	if result.Canceled {
		return attemptActionCanceled
	}
	if result.ResetConversation {
		return attemptActionResetConversation
	}
	if result.Written {
		return attemptActionWritten
	}
	return attemptActionContinue
}

func (h *relayHandler) handleSuccessfulAttempt(channel *dbmodel.Channel, key dbmodel.ChannelKey, plan *protocolroute.AttemptPlan, result attemptResult) bool {
	now := time.Now()
	availability.RecordCredentialSuccessRevision(channel.ID, key.ID, key.CredentialRevision, now)
	outlierwindow.Report(channel.ID, plan.UpstreamModel(), true, result.StatusCode, now)
	saveHTTPReplayState(httpReplaySaveInput{
		ctx: h.c.Request.Context(), inboundType: h.inboundType, request: h.request.internalRequest,
		inAdapter: h.request.inAdapter, metrics: h.metrics, previousState: h.replayState,
		apiKeyID: h.request.apiKeyID, groupID: h.group.ID, groupTTL: h.group.SessionKeepTime,
		requestModel: h.request.requestModel, channelID: channel.ID, channelKeyID: key.ID,
	})
	h.metrics.SaveWithChannelStats(h.c.Request.Context(), true, nil, h.iterator.Attempts(), false)
	return true
}
