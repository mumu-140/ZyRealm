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

func capabilityTestPlan(upstream protocol.Protocol) *protocolroute.AttemptPlan {
	return protocolroute.NewAttemptPlan(protocolroute.PlanSpec{
		ChannelID:        7,
		ChannelKeyID:     3,
		RequestedModel:   "group-model",
		UpstreamModel:    "upstream-model",
		IngressProtocol:  protocol.OpenAIChat,
		UpstreamProtocol: upstream,
		BaseURL:          "https://example.test/v1",
		HeaderPolicy: protocolroute.HeaderPolicy{Set: map[string]string{
			"X-Capability-Mode": "stable",
		}},
		AttemptKind: protocolroute.KindCandidatePrimary,
	})
}

func TestCapabilitySignatureTracksShapeNotPromptContent(t *testing.T) {
	firstText := "first prompt"
	secondText := "different prompt"
	plan := capabilityTestPlan(protocol.OpenAIChat)
	first := &transformerModel.InternalLLMRequest{
		Messages:        []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &firstText}}},
		ReasoningEffort: "xhigh",
	}
	second := &transformerModel.InternalLLMRequest{
		Messages:        []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &secondText}}},
		ReasoningEffort: "xhigh",
	}
	if got, want := capabilitySignature(first, plan), capabilitySignature(second, plan); got == "" || got != want {
		t.Fatalf("prompt content must not fragment capability cache: first=%q second=%q", got, want)
	}

	second.ReasoningEffort = "high"
	if capabilitySignature(first, plan) == capabilitySignature(second, plan) {
		t.Fatalf("reasoning effort values must produce distinct capability signatures")
	}

	second.ReasoningEffort = "xhigh"
	if capabilitySignature(first, plan) == capabilitySignature(second, capabilityTestPlan(protocol.OpenAIResponse)) {
		t.Fatalf("upstream protocol must be part of capability signature")
	}
}

func TestCapabilitySignatureSeparatesInputModalities(t *testing.T) {
	plan := capabilityTestPlan(protocol.OpenAIChat)
	image := &transformerModel.InternalLLMRequest{Messages: []transformerModel.Message{{
		Role:    "user",
		Content: transformerModel.MessageContent{MultipleContent: []transformerModel.MessageContentPart{{Type: "image_url"}}},
	}}}
	audio := &transformerModel.InternalLLMRequest{Messages: []transformerModel.Message{{
		Role:    "user",
		Content: transformerModel.MessageContent{MultipleContent: []transformerModel.MessageContentPart{{Type: "input_audio"}}},
	}}}
	if capabilitySignature(image, plan) == capabilitySignature(audio, plan) {
		t.Fatalf("image and audio input capability shapes must not share a negative cache key")
	}
}

func TestCapabilityConfigFingerprintIgnoresRuntimeSchedulingButTracksCapabilityConfig(t *testing.T) {
	plan := capabilityTestPlan(protocol.OpenAIChat)
	channel := &dbmodel.Channel{
		ID:             7,
		BaseUrls:       []dbmodel.BaseUrl{{URL: "https://example.test/v1"}},
		Model:          "upstream-model",
		MaxConcurrency: 1,
		MaxRPM:         10,
		Keys:           []dbmodel.ChannelKey{{ID: 1, ChannelKey: "first", TotalCost: 1}},
	}
	baseline := capabilityConfigFingerprint(channel, plan)
	if baseline == "" {
		t.Fatalf("expected non-empty config fingerprint")
	}

	channel.MaxConcurrency = 99
	channel.MaxRPM = 999
	channel.Keys = []dbmodel.ChannelKey{{ID: 2, ChannelKey: "rotated", TotalCost: 999}}
	if got := capabilityConfigFingerprint(channel, plan); got != baseline {
		t.Fatalf("credential/scheduling changes must not invalidate capability evidence")
	}

	channel.BaseUrls = []dbmodel.BaseUrl{{URL: "https://new.example.test/v1"}}
	if got := capabilityConfigFingerprint(channel, plan); got == baseline {
		t.Fatalf("upstream route configuration change must invalidate capability evidence")
	}
}

func TestFilterCapabilityNegativePlansUsesExactSignatureAndConfig(t *testing.T) {
	availability.Reset()
	t.Cleanup(availability.Reset)
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	text := "hello"
	request := &transformerModel.InternalLLMRequest{
		Messages:        []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &text}}},
		ReasoningEffort: "xhigh",
	}
	channel := &dbmodel.Channel{ID: 7, BaseUrls: []dbmodel.BaseUrl{{URL: "https://example.test/v1"}}, Model: "upstream-model"}
	plan := capabilityTestPlan(protocol.OpenAIChat)
	availability.RecordCapabilityNegative(
		channel.ID,
		plan.UpstreamModel(),
		capabilitySignature(request, plan),
		capabilityConfigFingerprint(channel, plan),
		"unsupported reasoning effort",
		now,
	)

	filtered, info, blocked := filterCapabilityNegativePlans(channel, request, []*protocolroute.AttemptPlan{plan}, now.Add(time.Second))
	if !blocked || len(filtered) != 0 || !info.Blocked {
		t.Fatalf("expected exact capability plan to be filtered, blocked=%v len=%d info=%+v", blocked, len(filtered), info)
	}

	request.ReasoningEffort = "high"
	filtered, _, blocked = filterCapabilityNegativePlans(channel, request, []*protocolroute.AttemptPlan{plan}, now.Add(time.Second))
	if blocked || len(filtered) != 1 {
		t.Fatalf("different capability signature must remain eligible, blocked=%v len=%d", blocked, len(filtered))
	}

	request.ReasoningEffort = "xhigh"
	channel.BaseUrls = []dbmodel.BaseUrl{{URL: "https://changed.example.test/v1"}}
	filtered, _, blocked = filterCapabilityNegativePlans(channel, request, []*protocolroute.AttemptPlan{plan}, now.Add(time.Second))
	if blocked || len(filtered) != 1 {
		t.Fatalf("changed channel config must bypass stale capability evidence, blocked=%v len=%d", blocked, len(filtered))
	}
}
