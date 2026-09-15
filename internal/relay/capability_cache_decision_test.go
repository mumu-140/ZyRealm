package relay

import (
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/protocol"
	"github.com/bestruirui/octopus/internal/protocolroute"
	"github.com/bestruirui/octopus/internal/relay/availability"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

func TestFilterCapabilityNegativePlansDetailedPreservesPartialSurvivor(t *testing.T) {
	availability.Reset()
	t.Cleanup(availability.Reset)
	now := time.Date(2026, 9, 15, 16, 30, 0, 0, time.UTC)
	text := "hello"
	request := &transformerModel.InternalLLMRequest{
		Messages:        []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &text}}},
		ReasoningEffort: "xhigh",
	}
	channel := &dbmodel.Channel{
		ID:       7,
		Name:     "capability-provider",
		BaseUrls: []dbmodel.BaseUrl{{URL: "https://example.test/v1"}},
		Model:    "upstream-model",
	}
	blockedPlan := capabilityTestPlan(protocol.OpenAIChat)
	survivorPlan := capabilityTestPlan(protocol.OpenAIResponse)
	availability.RecordCapabilityNegative(
		channel.ID,
		blockedPlan.UpstreamModel(),
		capabilitySignature(request, blockedPlan),
		capabilityConfigFingerprint(channel, blockedPlan),
		"unsupported reasoning effort",
		now,
	)

	result := filterCapabilityNegativePlansDetailed(
		channel,
		request,
		[]*protocolroute.AttemptPlan{blockedPlan, survivorPlan},
		now.Add(time.Second),
	)
	if !result.BlockedAny {
		t.Fatal("expected one exact capability rejection")
	}
	if len(result.Filtered) != 1 || result.Filtered[0] != survivorPlan {
		t.Fatalf("partial survivor changed: %#v", result.Filtered)
	}
	if len(result.Rejected) != 1 || result.Rejected[0].Plan != blockedPlan {
		t.Fatalf("rejected plan snapshot = %#v", result.Rejected)
	}
	if !result.Rejected[0].Info.Blocked || result.Rejected[0].Info.Reason != "unsupported reasoning effort" {
		t.Fatalf("rejected capability info = %#v", result.Rejected[0].Info)
	}
	if !result.Nearest.Blocked {
		t.Fatalf("nearest blocked snapshot lost: %#v", result.Nearest)
	}
}

func TestCapabilityNegativeDecisionEventUsesPersistedSafeMetadata(t *testing.T) {
	now := time.Date(2026, 9, 15, 16, 30, 0, 0, time.UTC)
	plan := capabilityTestPlan(protocol.OpenAIResponse)
	channel := &dbmodel.Channel{ID: 7, Name: "capability-provider"}
	key := dbmodel.ChannelKey{ID: 3}
	rejection := capabilityPlanRejection{
		Plan: plan,
		Info: availability.CapabilitySnapshot{
			Blocked:   true,
			Reason:    "unsupported response protocol",
			ExpiresAt: now.Add(30 * time.Minute),
		},
	}

	event := capabilityNegativeDecisionEvent(channel, key, rejection)
	if event.Stage != dbmodel.DecisionStageProtocol || event.Outcome != dbmodel.DecisionOutcomeRejected {
		t.Fatalf("capability event = stage %q outcome %q", event.Stage, event.Outcome)
	}
	if event.Reason != dbmodel.DecisionReasonCapabilityNegative {
		t.Fatalf("capability event reason = %q", event.Reason)
	}
	if event.ChannelID != 7 || event.ChannelKeyID != 3 || event.ChannelName != "capability-provider" {
		t.Fatalf("capability event identity = %#v", event)
	}
	if event.ModelName != plan.UpstreamModel() || event.Protocol != string(plan.UpstreamProtocol()) {
		t.Fatalf("capability event route metadata = %#v", event)
	}
	if event.ExpiresAt != rejection.Info.ExpiresAt.Unix() || event.Detail != rejection.Info.Reason {
		t.Fatalf("capability event evidence = %#v", event)
	}
}
