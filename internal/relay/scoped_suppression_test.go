package relay

import (
	"context"
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/protocol"
	"github.com/bestruirui/octopus/internal/protocolroute"
	"github.com/bestruirui/octopus/internal/relay/availability"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

func scopedSuppressionRequest(effort string, verbosity *string) *transformerModel.InternalLLMRequest {
	text := "hello"
	return &transformerModel.InternalLLMRequest{
		Messages:        []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &text}}},
		ReasoningEffort: effort,
		Verbosity:       verbosity,
	}
}

func filterSingleCapabilityPlan(
	channel *dbmodel.Channel,
	request *transformerModel.InternalLLMRequest,
	plan *protocolroute.AttemptPlan,
	now time.Time,
) capabilityFilterResult {
	return filterCapabilityNegativePlansDetailed(channel, request, []*protocolroute.AttemptPlan{plan}, now)
}

func TestRoutingDecisionClassifiesModelNotPricedAsProviderModelCapability(t *testing.T) {
	result := attemptResult{
		StatusCode:        400,
		UpstreamErrorBody: `{"error":{"message":"model is not priced","type":"invalid_request_error"}}`,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 7, result)
	if decision.Domain != failureDomainModelCapability {
		t.Fatalf("model is not priced domain = %v, want model capability", decision.Domain)
	}
	if decision.RuleID != "model_not_priced" {
		t.Fatalf("model is not priced rule = %q, want model_not_priced", decision.RuleID)
	}
	if decision.FailureScope != routingScopeProviderModel {
		t.Fatalf("model is not priced scope = %q, want provider_model", decision.FailureScope)
	}
}

func TestRoutingDecisionIdentifiesReasoningEffortCapability(t *testing.T) {
	result := attemptResult{
		StatusCode:        400,
		UpstreamErrorBody: `{"error":{"message":"Model glm-5.3 does not support reasoning effort xhigh","code":"effort_not_supported"}}`,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 7, result)
	if decision.Domain != failureDomainModelCapability {
		t.Fatalf("reasoning effort domain = %v, want model capability", decision.Domain)
	}
	if decision.RuleID != "reasoning_effort_unsupported" {
		t.Fatalf("reasoning effort rule = %q, want reasoning_effort_unsupported", decision.RuleID)
	}
}

func TestScopedSuppressionProviderModelBlocksDifferentRequestShape(t *testing.T) {
	availability.Reset()
	t.Cleanup(availability.Reset)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	channel := &dbmodel.Channel{
		ID:       7,
		BaseUrls: []dbmodel.BaseUrl{{URL: "https://example.test/v1"}},
		Model:    "upstream-model",
	}
	plan := capabilityTestPlan(protocol.OpenAIChat)
	first := scopedSuppressionRequest("", nil)
	verbosity := "high"
	changedShape := scopedSuppressionRequest("", &verbosity)

	recordCapabilityNegative(channel, first, plan, attemptResult{
		StatusCode:        400,
		UpstreamErrorBody: `{"error":{"message":"model is not priced"}}`,
		Decision: RoutingDecision{
			Valid:        true,
			Domain:       failureDomainModelCapability,
			RuleID:       "model_not_priced",
			FailureScope: routingScopeProviderModel,
		},
	}, now)

	result := filterSingleCapabilityPlan(channel, changedShape, plan, now.Add(time.Second))
	if !result.BlockedAny || len(result.Filtered) != 0 {
		t.Fatalf("provider-model suppression must survive unrelated request-shape changes: blocked=%v filtered=%d", result.BlockedAny, len(result.Filtered))
	}
}

func TestScopedSuppressionReasoningEffortBlocksSameFeatureOnly(t *testing.T) {
	availability.Reset()
	t.Cleanup(availability.Reset)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	channel := &dbmodel.Channel{
		ID:       7,
		BaseUrls: []dbmodel.BaseUrl{{URL: "https://example.test/v1"}},
		Model:    "upstream-model",
	}
	plan := capabilityTestPlan(protocol.OpenAIChat)
	first := scopedSuppressionRequest("xhigh", nil)
	verbosity := "high"
	sameFeatureDifferentShape := scopedSuppressionRequest("xhigh", &verbosity)
	differentFeature := scopedSuppressionRequest("high", &verbosity)

	recordCapabilityNegative(channel, first, plan, attemptResult{
		StatusCode:        400,
		UpstreamErrorBody: `{"error":{"message":"reasoning effort xhigh is not supported"}}`,
		Decision: RoutingDecision{
			Valid:        true,
			Domain:       failureDomainModelCapability,
			RuleID:       "reasoning_effort_unsupported",
			FailureScope: routingScopeProviderModel,
		},
	}, now)

	same := filterSingleCapabilityPlan(channel, sameFeatureDifferentShape, plan, now.Add(time.Second))
	if !same.BlockedAny || len(same.Filtered) != 0 {
		t.Fatalf("same unsupported reasoning effort must be suppressed across unrelated shape changes: blocked=%v filtered=%d", same.BlockedAny, len(same.Filtered))
	}

	other := filterSingleCapabilityPlan(channel, differentFeature, plan, now.Add(time.Second))
	if other.BlockedAny || len(other.Filtered) != 1 {
		t.Fatalf("different reasoning effort must remain eligible: blocked=%v filtered=%d", other.BlockedAny, len(other.Filtered))
	}
}

func TestScopedSuppressionGenericCapabilityRemainsRequestShapeScoped(t *testing.T) {
	availability.Reset()
	t.Cleanup(availability.Reset)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	channel := &dbmodel.Channel{
		ID:       7,
		BaseUrls: []dbmodel.BaseUrl{{URL: "https://example.test/v1"}},
		Model:    "upstream-model",
	}
	plan := capabilityTestPlan(protocol.OpenAIChat)
	first := scopedSuppressionRequest("", nil)
	verbosity := "high"
	changedShape := scopedSuppressionRequest("", &verbosity)

	recordCapabilityNegative(channel, first, plan, attemptResult{
		StatusCode:        400,
		UpstreamErrorBody: `{"error":{"message":"unsupported parameter: prompt_cache_key"}}`,
		Decision: RoutingDecision{
			Valid:        true,
			Domain:       failureDomainModelCapability,
			RuleID:       "model_capability",
			FailureScope: routingScopeProviderModel,
		},
	}, now)

	exact := filterSingleCapabilityPlan(channel, first, plan, now.Add(time.Second))
	if !exact.BlockedAny || len(exact.Filtered) != 0 {
		t.Fatalf("exact request-shape capability evidence must still suppress the matching shape")
	}
	changed := filterSingleCapabilityPlan(channel, changedShape, plan, now.Add(time.Second))
	if changed.BlockedAny || len(changed.Filtered) != 1 {
		t.Fatalf("generic capability evidence must not overblock unrelated shapes: blocked=%v filtered=%d", changed.BlockedAny, len(changed.Filtered))
	}
}
