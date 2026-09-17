package relay

import (
	"strings"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/protocolroute"
	"github.com/bestruirui/octopus/internal/relay/availability"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

const providerModelSuppressionSignature = "suppress:v1:provider_model"

func reasoningEffortSuppressionSignature(request *transformerModel.InternalLLMRequest) string {
	if request == nil {
		return ""
	}
	effort := strings.ToLower(strings.TrimSpace(request.ReasoningEffort))
	if effort == "" {
		return ""
	}
	return "suppress:v1:feature:reasoning_effort=" + effort
}

func capabilitySuppressionLookupSignatures(request *transformerModel.InternalLLMRequest, plan *protocolroute.AttemptPlan) []string {
	if request == nil || plan == nil {
		return nil
	}
	signatures := []string{providerModelSuppressionSignature}
	if feature := reasoningEffortSuppressionSignature(request); feature != "" {
		signatures = append(signatures, feature)
	}
	return append(signatures, capabilitySignature(request, plan))
}

func capabilitySuppressionRecordSignature(decision RoutingDecision, request *transformerModel.InternalLLMRequest, plan *protocolroute.AttemptPlan) string {
	switch decision.RuleID {
	case "model_not_priced":
		return providerModelSuppressionSignature
	case "reasoning_effort_unsupported":
		if feature := reasoningEffortSuppressionSignature(request); feature != "" {
			return feature
		}
	}
	return capabilitySignature(request, plan)
}

func capabilitySuppressionInfo(
	channel *dbmodel.Channel,
	request *transformerModel.InternalLLMRequest,
	plan *protocolroute.AttemptPlan,
	now time.Time,
) availability.CapabilitySnapshot {
	if channel == nil || request == nil || plan == nil {
		return availability.CapabilitySnapshot{}
	}
	configFingerprint := capabilityConfigFingerprint(channel, plan)
	for _, signature := range capabilitySuppressionLookupSignatures(request, plan) {
		info := availability.CapabilityInfo(channel.ID, plan.UpstreamModel(), signature, configFingerprint, now)
		if info.Blocked {
			return info
		}
	}
	return availability.CapabilitySnapshot{}
}

func recordCapabilitySuppression(
	channel *dbmodel.Channel,
	request *transformerModel.InternalLLMRequest,
	plan *protocolroute.AttemptPlan,
	result attemptResult,
	now time.Time,
) time.Time {
	if channel == nil || request == nil || plan == nil {
		return time.Time{}
	}
	return availability.RecordCapabilityNegative(
		channel.ID,
		plan.UpstreamModel(),
		capabilitySuppressionRecordSignature(result.Decision, request, plan),
		capabilityConfigFingerprint(channel, plan),
		compactCapabilityReason(outlierErrorText(result.Err, result.UpstreamErrorBody)),
		now,
	)
}

func clearCapabilitySuppressions(channel *dbmodel.Channel, request *transformerModel.InternalLLMRequest, plan *protocolroute.AttemptPlan) {
	if channel == nil || request == nil || plan == nil {
		return
	}
	configFingerprint := capabilityConfigFingerprint(channel, plan)
	for _, signature := range capabilitySuppressionLookupSignatures(request, plan) {
		availability.ClearCapabilityNegative(channel.ID, plan.UpstreamModel(), signature, configFingerprint)
	}
}
