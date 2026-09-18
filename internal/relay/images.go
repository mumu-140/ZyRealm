package relay

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/outlierwindow"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/relay/bodycache"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

// ImagesHandler 是 OpenAI Images API 的统一 relay 入口。
// endpoint 形如：/images/generations、/images/edits、/images/variations（不含 /v1 前缀）。
func ImagesHandler(endpoint string, c *gin.Context) {
	ctx := c.Request.Context()
	apiKeyID := c.GetInt("api_key_id")

	bc, err := bodycache.New(c.Request.Body)
	if err != nil {
		var tooLarge *bodycache.BodyTooLargeError
		if errors.As(err, &tooLarge) {
			resp.Error(c, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() {
		if cerr := bc.Close(); cerr != nil {
			log.Warnf("failed to close images body cache: %v", cerr)
		}
	}()

	contentType := c.GetHeader("Content-Type")
	isMultipart := strings.Contains(strings.ToLower(contentType), "multipart/form-data")

	var (
		requestModel string
		stream       bool
		boundary     string
		jsonPayload  map[string]any
	)
	if isMultipart {
		_, params, perr := mime.ParseMediaType(contentType)
		if perr != nil {
			resp.Error(c, http.StatusBadRequest, "invalid multipart content-type")
			return
		}
		boundary = strings.TrimSpace(params["boundary"])
		if boundary == "" {
			resp.Error(c, http.StatusBadRequest, "invalid multipart boundary")
			return
		}
		m, s, perr := parseMultipartModelAndStream(bc, boundary)
		if perr != nil {
			resp.Error(c, http.StatusBadRequest, perr.Error())
			return
		}
		requestModel = m
		stream = s
	} else {
		payload, m, s, perr := parseJSONModelAndStream(bc)
		if perr != nil {
			resp.Error(c, http.StatusBadRequest, perr.Error())
			return
		}
		jsonPayload = payload
		requestModel = m
		stream = s
	}

	supportedModels := strings.TrimSpace(c.GetString("supported_models"))
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		if !slices.Contains(supportedModelsArray, requestModel) {
			resp.ErrorWithCode(c, http.StatusBadRequest, CodeRelayModelNotSupported, "model not supported")
			return
		}
	}

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
	policyRequest := &relayRequest{ctx: ctx, iter: iter, requestModel: requestModel}

	metrics := newImagesRelayMetrics(apiKeyID, requestModel)
	metrics.RequestContent = buildImagesRequestContentForLog(isMultipart, bc, jsonPayload)

	hb := startEarlyHeartbeat(c, stream)
	defer hb.Stop()

	var (
		lastErr         error
		capacitySkipped bool
		rateSkipped     bool
	)

	maxUpstreamStarts := 1
	if group.RetryEnabled && group.MaxRetries > 0 {
		maxUpstreamStarts = group.MaxRetries + 1
	}
	upstreamStarts := 0

	for iter.Next() && upstreamStarts < maxUpstreamStarts {
		select {
		case <-ctx.Done():
			log.Debugf("request context canceled, stopping retry")
			metrics.SaveWithChannelStats(ctx, false, context.Canceled, iter.Attempts(), false)
			return
		default:
		}

		item := iter.Item()

		channel, err := op.ChannelGet(item.ChannelID, ctx)
		if err != nil {
			log.Warnf("failed to get channel %d: %v", item.ChannelID, err)
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}

		if channel.Type != outbound.OutboundTypeOpenAIChat && channel.Type != outbound.OutboundTypeOpenAIResponse {
			iter.Skip(channel.ID, 0, channel.Name, fmt.Sprintf("unsupported channel type: %d", channel.Type))
			continue
		}

		upstreamModel := balancer.ItemUpstreamModel(item, requestModel)
		runtimeLease, runtimeEligible := availability.AcquireCandidate(channel.ID, upstreamModel, time.Now())
		if !runtimeEligible {
			iter.Skip(channel.ID, 0, channel.Name, "runtime cooldown or half-open lease busy")
			continue
		}

		if !balancer.TryAcquireChannel(channel.ID, channel.MaxConcurrency) {
			availability.ReleaseLease(runtimeLease, time.Now())
			capacitySkipped = true
			iter.SkipCapacity(channel.ID, 0, channel.Name,
				fmt.Sprintf("channel at max concurrency (%d)", channel.MaxConcurrency))
			continue
		}
		if !balancer.TryConsumeChannelRPM(channel.ID, channel.MaxRPM, time.Now()) {
			balancer.ReleaseChannel(channel.ID)
			availability.ReleaseLease(runtimeLease, time.Now())
			rateSkipped = true
			iter.SkipRateLimit(channel.ID, 0, channel.Name,
				fmt.Sprintf("channel at max rpm (%d)", channel.MaxRPM))
			continue
		}

		// 同渠道内按 Key 重试（容量 slot 覆盖全部 Key 尝试）。Images 保留原有
		// GetChannelKey 排序，只把 credential/runtime eligibility 迁到统一 authority。
		// outlierwindow 仍只记录渠道最终结果，不记录中间 Key 失败。
		done := func() bool {
			defer balancer.ReleaseChannel(channel.ID)
			defer availability.ReleaseLease(runtimeLease, time.Now())

			excludeKeys := make(map[int]struct{})
			preferredKeyID := 0
			if iter.IsSticky() {
				preferredKeyID = iter.StickyKeyID()
			}

			var channelLastErr error
			var channelLastStatus int
			var channelLastResult attemptResult

			for upstreamStarts < maxUpstreamStarts {
				selectOpts := model.ChannelKeySelectOptions{
					ExcludeKeyIDs:  excludeKeys,
					PreferredKeyID: preferredKeyID,
				}
				usedKey := channel.GetChannelKey(selectOpts)
				if usedKey.ChannelKey == "" {
					break
				}
				if !availability.CredentialAvailableRevision(channel.ID, usedKey.ID, usedKey.CredentialRevision, time.Now()) {
					excludeKeys[usedKey.ID] = struct{}{}
					preferredKeyID = 0
					iter.RecordDecision(model.RoutingDecisionEvent{
						Stage:        model.DecisionStageCredential,
						Outcome:      model.DecisionOutcomeRejected,
						Reason:       model.DecisionReasonCredentialCooldown,
						ChannelID:    channel.ID,
						ChannelKeyID: usedKey.ID,
						ChannelName:  channel.Name,
					})
					continue
				}

				log.Debugf("images request model %s, mode: %d, forwarding to channel: %s model: %s (attempt %d/%d, sticky=%t, stream=%t)",
					requestModel, group.Mode, channel.Name, upstreamModel,
					iter.Index()+1, iter.Len(), iter.IsSticky(), stream)

				upstreamStarts++
				span := iter.StartAttempt(channel.ID, usedKey.ID, channel.Name)

				statusCode, written, usage, upstreamCT, fwdErr := imagesAttempt(ctx, endpoint, c, bc, isMultipart, boundary, jsonPayload, stream, channel, usedKey.ChannelKey, group.FirstTokenTimeOut, metrics, upstreamModel, hb)

				usedKey.StatusCode = statusCode
				usedKey.LastUseTimeStamp = time.Now().Unix()

				result := imagesAttemptRoutingResult(ctx, statusCode, written, fwdErr, span)
				result = withRoutingDecision(ctx, policyRequest, channel.ID, result)
				now := time.Now()
				recordRuntimeAvailabilityEvidence(ctx, channel.ID, upstreamModel, result, now)
				decision := result.Decision

				if ctx.Err() != nil {
					metrics.SaveWithChannelStats(ctx, false, ctx.Err(), iter.Attempts(), false)
					return true
				}

				if fwdErr == nil {
					availability.RecordCredentialSuccessRevision(channel.ID, usedKey.ID, usedKey.CredentialRevision, now)
					// 成功：记录渠道-模型成功样本
					outlierwindow.Report(channel.ID, upstreamModel, true, statusCode, now)
					metrics.ActualModel = upstreamModel
					if usage != nil {
						metrics.SetUsageFromImages(upstreamModel, *usage)
					}
					metrics.ResponseContent = buildImagesResponseContentForLog(stream, upstreamCT, usage)
					usedKey.TotalCost += metrics.Stats.InputCost + metrics.Stats.OutputCost
					op.ChannelKeyUpdate(usedKey)
					span.End(model.AttemptSuccess, statusCode, "")
					op.StatsChannelUpdate(channel.ID, model.StatsMetrics{
						WaitTime:       span.Duration().Milliseconds(),
						RequestSuccess: 1,
					})
					balancer.SetSticky(apiKeyID, requestModel, channel.ID, usedKey.ID)
					metrics.SaveWithChannelStats(ctx, true, nil, iter.Attempts(), false)
					return true
				}

				op.ChannelKeyUpdate(usedKey)
				span.End(model.AttemptFailed, statusCode, fwdErr.Error())
				op.StatsChannelUpdate(channel.ID, model.StatsMetrics{
					WaitTime:      span.Duration().Milliseconds(),
					RequestFailed: 1,
				})

				// 已写出：无法再重试。RoutingDecision 仍可把已提交的上游传输失败
				// 作为 future-health 证据，但 legacy circuit 不重新获得准入权。
				if written {
					reportOutlierDecision(channel.ID, upstreamModel, decision.OutlierScope, statusCode, now)
					metrics.SaveWithChannelStats(ctx, false, fwdErr, iter.Attempts(), false)
					return true
				}

				if decision.Domain == failureDomainCredential {
					recordCredentialRoutingFailureRevision(channel.ID, usedKey.ID, usedKey.CredentialRevision, result, now)
				}
				channelLastErr = fmt.Errorf("channel %s key %d failed: %w", channel.Name, usedKey.ID, fwdErr)
				channelLastStatus = statusCode
				channelLastResult = result

				// 保留 Images 原有 HTTP retry 语义；P4B 只补 credential-domain 的同渠道换 Key。
				if group.RetryEnabled && (decision.Domain == failureDomainCredential || isRetryableStatus(statusCode)) {
					excludeKeys[usedKey.ID] = struct{}{}
					preferredKeyID = 0
					continue
				}
				break
			}

			// 渠道所有 Key 均失败：只按最终 RoutingDecision 的作用域记录一次失败样本。
			if channelLastErr != nil {
				reportOutlierDecision(channel.ID, upstreamModel, channelLastResult.Decision.OutlierScope, channelLastStatus, time.Now())
				lastErr = channelLastErr
			}
			return false
		}()

		if done {
			return
		}

		capacitySkipped = false
		rateSkipped = false
	}

	if capacitySkipped && lastErr == nil {
		lastErr = &imagesUpstreamError{
			StatusCode: http.StatusServiceUnavailable,
			RetryAfter: "1",
			Message:    "all channels at capacity",
		}
	}
	if rateSkipped && lastErr == nil {
		lastErr = &imagesUpstreamError{
			StatusCode: http.StatusTooManyRequests,
			RetryAfter: "60",
			Message:    "all channels rate limited",
		}
	}
	metrics.SaveWithChannelStats(ctx, false, lastErr, iter.Attempts(), false)
	writeFinalImagesError(c, hb, lastErr)
}
