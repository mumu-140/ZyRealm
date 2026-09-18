package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/outlierwindow"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/resp"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

type responsesCompactRequest struct {
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input,omitempty"`
	PreviousResponseID *string         `json:"previous_response_id,omitempty"`
}

type responsesCompactResponse struct {
	ID        string                         `json:"id"`
	Object    string                         `json:"object"`
	CreatedAt int64                          `json:"created_at"`
	Output    []openaiOutbound.ResponsesItem `json:"output"`
	Usage     *openaiOutbound.ResponsesUsage `json:"usage,omitempty"`
	Error     *transformerModel.ErrorDetail  `json:"error,omitempty"`
}

// HandleResponsesCompact proxies OpenAI-compatible /responses/compact requests upstream.
func HandleResponsesCompact(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	var compactReq responsesCompactRequest
	if err := json.Unmarshal(body, &compactReq); err != nil {
		resp.Error(c, http.StatusBadRequest, fmt.Sprintf("failed to decode responses compact request: %v", err))
		return
	}
	if strings.TrimSpace(compactReq.Model) == "" {
		resp.Error(c, http.StatusBadRequest, "model is required")
		return
	}
	if len(compactReq.Input) == 0 && compactReq.PreviousResponseID == nil {
		resp.Error(c, http.StatusBadRequest, "either input or previous_response_id is required")
		return
	}

	supportedModels := c.GetString("supported_models")
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		if !slices.Contains(supportedModelsArray, compactReq.Model) {
			resp.ErrorWithCode(c, http.StatusBadRequest, CodeRelayModelNotSupported, "model not supported")
			return
		}
	}

	// 上游模型名可能与请求模型名不同（渠道-模型映射写在 GroupItem.ModelName 上），
	// 每个候选都要按自己的上游模型名重写 body 的 model 字段，
	// 所以这里保留完整字段集合，而不是只留已知字段的 compactReq。
	var compactPayload map[string]json.RawMessage
	if err := json.Unmarshal(body, &compactPayload); err != nil {
		resp.Error(c, http.StatusBadRequest, fmt.Sprintf("failed to decode responses compact request: %v", err))
		return
	}

	requestModel := compactReq.Model
	apiKeyID := c.GetInt("api_key_id")
	ctx := c.Request.Context()

	group, err := op.GroupGetEnabledMap(requestModel, ctx)
	if err != nil {
		resp.ErrorWithCode(c, http.StatusNotFound, CodeRelayModelNotFound, "model not found")
		return
	}

	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}

	metricsReq := &transformerModel.InternalLLMRequest{Model: requestModel, RawRequest: body}
	metrics := NewRelayMetrics(apiKeyID, requestModel, body, metricsReq)
	policyRequest := &relayRequest{ctx: ctx, requestModel: requestModel, iter: iter}

	var lastErr error
	var lastStatusCode int
	var lastRetryAfter time.Duration
	var stopRouting bool

	maxSameChannelRetries := 1
	if group.RetryEnabled {
		maxSameChannelRetries = group.MaxRetries
		if maxSameChannelRetries <= 0 {
			maxSameChannelRetries = 3
		}
	}

	for iter.Next() {
		select {
		case <-ctx.Done():
			log.Infof("compact request context canceled, stopping retry")
			metrics.SaveWithChannelStats(ctx, false, context.Canceled, iter.Attempts(), false)
			return
		default:
		}

		item := iter.Item()
		upstreamModel := balancer.ItemUpstreamModel(item, requestModel)
		channel, err := op.ChannelGet(item.ChannelID, ctx)
		if err != nil {
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}
		if !supportsResponsesCompact(channel.Type) {
			iter.Skip(channel.ID, 0, channel.Name, "channel type not compatible with responses compact")
			continue
		}

		runtimeLease, runtimeEligible := availability.AcquireCandidate(channel.ID, upstreamModel, time.Now())
		if !runtimeEligible {
			iter.Skip(channel.ID, 0, channel.Name, "runtime cooldown or half-open lease busy")
			continue
		}

		done := func() bool {
			defer availability.ReleaseLease(runtimeLease, time.Now())

			selectOpts := dbmodel.ChannelKeySelectOptions{
				ExcludeKeyIDs:  make(map[int]struct{}),
				PreferredKeyID: iter.StickyKeyID(),
			}
			var usedKey dbmodel.ChannelKey
			selectNextCredential := func() bool {
				candidate := selectFairChannelCredential(channel, selectOpts, iter, time.Now())
				if candidate.ChannelKey == "" {
					return false
				}
				usedKey = candidate
				return true
			}
			if !selectNextCredential() {
				if len(selectOpts.ExcludeKeyIDs) == 0 {
					iter.Skip(channel.ID, 0, channel.Name, "no available key")
				}
				return false
			}

			attemptBody, err := compactBodyForModel(compactPayload, upstreamModel)
			if err != nil {
				iter.Skip(channel.ID, usedKey.ID, channel.Name, err.Error())
				lastErr = err
				return false
			}

			var attemptErr error
			var statusCode int
			var retryAfter time.Duration
			var success bool
			var result attemptResult
			var coordination attemptCoordination
			delaySameCredential := false

		attemptLoop:
			for retryNum := 0; retryNum < maxSameChannelRetries; retryNum++ {
				if retryNum > 0 && delaySameCredential {
					delay := computeBackoff(retryNum, retryAfter)
					select {
					case <-ctx.Done():
						metrics.SaveWithChannelStats(ctx, false, context.Canceled, iter.Attempts(), false)
						return true
					case <-time.After(delay):
					}
				}

				var span *balancer.AttemptSpan
				statusCode, retryAfter, attemptErr, span = forwardResponsesCompact(c, metrics, iter, channel, usedKey, attemptBody, upstreamModel)
				result = compactAttemptRoutingResult(ctx, policyRequest, channel.ID, statusCode, retryAfter, attemptErr, span)
				coordination, _ = coordinateAttemptOutcome(result) // compactAttemptRoutingResult guarantees a valid verdict
				attachSidepathRoutingTrace(ctx, result, usedKey.CredentialRevision)
				if attemptErr == nil {
					success = true
					break
				}

				switch resolveSidepathDirective(coordination.Disposition) {
				case sidepathDirectiveRetrySameCredential:
					if retryNum+1 < maxSameChannelRetries {
						delaySameCredential = true
						continue
					}
				case sidepathDirectiveRotateCredential:
					recordCredentialRoutingFailureRevision(channel.ID, usedKey.ID, usedKey.CredentialRevision, result, time.Now())
					selectOpts.ExcludeKeyIDs[usedKey.ID] = struct{}{}
					selectOpts.PreferredKeyID = 0
					if retryNum+1 < maxSameChannelRetries && selectNextCredential() {
						delaySameCredential = false
						continue
					}
				case sidepathDirectiveNextProvider:
					iter.SkipProvider(channel.ID)
				case sidepathDirectiveStop:
					stopRouting = true
				}
				break attemptLoop
			}

			usedKey.StatusCode = statusCode
			usedKey.LastUseTimeStamp = time.Now().Unix()
			op.ChannelKeyUpdate(usedKey)

			now := time.Now()
			applyRuntimeAvailabilityEffect(channel.ID, upstreamModel, result, coordination.Effects, now)

			if success {
				availability.RecordCredentialSuccessRevision(channel.ID, usedKey.ID, usedKey.CredentialRevision, now)
				op.StatsChannelUpdate(channel.ID, dbmodel.StatsMetrics{RequestSuccess: 1})
				// 粘性会话按请求模型存取：Iterator.GetSticky 用的是请求模型名，
				// 换成上游模型名会导致写进去的粘性记录读不到。
				balancer.SetSticky(apiKeyID, requestModel, channel.ID, usedKey.ID)
				outlierwindow.Report(channel.ID, upstreamModel, true, statusCode, now)
				metrics.SaveWithChannelStats(ctx, true, nil, iter.Attempts(), false)
				return true
			}

			op.StatsChannelUpdate(channel.ID, dbmodel.StatsMetrics{RequestFailed: 1})
			reportOutlierDecision(channel.ID, upstreamModel, coordination.Effects.OutlierScope, statusCode, now)
			lastErr = attemptErr
			lastStatusCode = statusCode
			lastRetryAfter = retryAfter
			return false
		}()
		if done {
			return
		}
		if stopRouting {
			break
		}
	}

	metrics.SaveWithChannelStats(ctx, false, lastErr, iter.Attempts(), false)
	if lastErr == nil && lastStatusCode == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}
	if isPassthroughStatus(lastStatusCode) {
		if lastRetryAfter > 0 {
			c.Header("Retry-After", fmt.Sprintf("%d", int(lastRetryAfter.Seconds())))
		}
		resp.Error(c, lastStatusCode, "channel failed")
		return
	}
	if lastStatusCode > 0 {
		resp.Error(c, lastStatusCode, "channel failed")
		return
	}
	resp.Error(c, http.StatusBadGateway, "channel failed")
}

func supportsResponsesCompact(channelType outbound.OutboundType) bool {
	switch channelType {
	case outbound.OutboundTypeOpenAIResponse:
		return true
	default:
		return false
	}
}

// forwardResponsesCompact 发一次上游请求。requestBody 已按 upstreamModel 改写过 model 字段，
// upstreamModel 同时作为计量口径：主链路（relay_request.go）也是用实际发出去的上游模型名
// 计 token 与实际模型，compact 用请求模型名会让日志里的「实际使用模型」变成客户端模型名。
func forwardResponsesCompact(c *gin.Context, metrics *RelayMetrics, iter *balancer.Iterator, channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, requestBody []byte, upstreamModel string) (int, time.Duration, error, *balancer.AttemptSpan) {
	span := iter.StartAttempt(channel.ID, usedKey.ID, channel.Name)
	request, err := buildResponsesCompactRequest(c.Request.Context(), channel, usedKey.ChannelKey, requestBody)
	if err != nil {
		span.End(dbmodel.AttemptFailed, 0, err.Error())
		return 0, 0, fmt.Errorf("failed to create compact request: %w", err), span
	}
	metrics.SetTransportRequestPayload(requestBody, upstreamModel)
	copyProxyHeaders(c.Request.Header, channel, request.Header)

	response, err := sendCompactRequest(channel, request)
	if err != nil {
		span.End(dbmodel.AttemptFailed, 0, err.Error())
		return 0, 0, fmt.Errorf("failed to send compact request: %w", err), span
	}
	defer response.Body.Close()

	body, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		span.End(dbmodel.AttemptFailed, response.StatusCode, readErr.Error())
		return response.StatusCode, 0, fmt.Errorf("failed to read compact response body: %w", readErr), span
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryAfter := parseRetryAfter(response.Header.Get("Retry-After"))
		statusCode := normalizeUpstreamStatusCode(response.StatusCode, string(body))
		span.End(dbmodel.AttemptFailed, statusCode, string(body))
		return statusCode, retryAfter, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(body)), span
	}

	copyProxyResponseHeaders(c.Writer.Header(), response.Header)
	contentType := response.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}
	c.Data(response.StatusCode, contentType, body)

	var compactResp responsesCompactResponse
	if err := json.Unmarshal(body, &compactResp); err == nil {
		metrics.SetInternalResponse(compactResponseToInternalResponse(&compactResp), upstreamModel)
	}

	span.End(dbmodel.AttemptSuccess, response.StatusCode, "")
	return response.StatusCode, 0, nil, span
}

// compactBodyForModel 把请求体的 model 字段改写为该候选的上游模型名，其余字段原样保留。
// 走 map[string]json.RawMessage 而不是 responsesCompactRequest：后者只有三个已知字段，
// 重新序列化会静默丢掉客户端传来的其他参数（tools、metadata、reasoning 等）。
func compactBodyForModel(payload map[string]json.RawMessage, upstreamModel string) ([]byte, error) {
	if payload == nil {
		return nil, errors.New("nil compact payload")
	}
	encodedModel, err := json.Marshal(upstreamModel)
	if err != nil {
		return nil, fmt.Errorf("failed to encode upstream model: %w", err)
	}
	next := make(map[string]json.RawMessage, len(payload))
	for k, v := range payload {
		next[k] = v
	}
	next["model"] = encodedModel
	body, err := json.Marshal(next)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal compact request: %w", err)
	}
	return body, nil
}

func buildResponsesCompactRequest(ctx context.Context, channel *dbmodel.Channel, key string, requestBody []byte) (*http.Request, error) {
	parsedURL, err := url.Parse(strings.TrimSuffix(channel.GetBaseUrl(), "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedURL.Path = parsedURL.Path + "/responses/compact"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedURL.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	return req, nil
}

func copyProxyHeaders(src http.Header, channel *dbmodel.Channel, dst http.Header) {
	for key, values := range src {
		lowerKey := strings.ToLower(key)
		if hopByHopHeaders[lowerKey] || lowerKey == "content-type" {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	for _, header := range channel.CustomHeader {
		if strings.EqualFold(header.HeaderKey, "Content-Type") {
			continue
		}
		dst.Set(header.HeaderKey, header.HeaderValue)
	}
	// 防止 Go 默认 User-Agent 泄露到上游
	if dst.Get("User-Agent") == "" {
		dst.Set("User-Agent", "")
	}
}

func copyProxyResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		dst.Del(key)
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func sendCompactRequest(channel *dbmodel.Channel, req *http.Request) (*http.Response, error) {
	httpClient, err := helper.ChannelHTTPClientWithContext(req.Context(), channel)
	if err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}

func compactResponseToInternalResponse(resp *responsesCompactResponse) *transformerModel.InternalLLMResponse {
	if resp == nil {
		return nil
	}
	return &transformerModel.InternalLLMResponse{
		ID:      resp.ID,
		Object:  resp.Object,
		Created: resp.CreatedAt,
		Usage:   convertCompactUsage(resp.Usage),
	}
}

func convertCompactUsage(usage *openaiOutbound.ResponsesUsage) *transformerModel.Usage {
	if usage == nil {
		return nil
	}
	result := &transformerModel.Usage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.TotalTokens,
	}
	if usage.InputTokenDetails.CachedTokens > 0 {
		result.PromptTokensDetails = &transformerModel.PromptTokensDetails{
			CachedTokens: usage.InputTokenDetails.CachedTokens,
		}
	}
	if usage.OutputTokenDetails.ReasoningTokens > 0 {
		result.CompletionTokensDetails = &transformerModel.CompletionTokensDetails{
			ReasoningTokens: usage.OutputTokenDetails.ReasoningTokens,
		}
	}
	return result
}
