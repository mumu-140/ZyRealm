package relay

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestDecisionOnlyAttemptDoesNotAffectFinalRouteResolution(t *testing.T) {
	decisionOnly := model.ChannelAttempt{
		ChannelID:   99,
		ChannelName: "filtered-provider",
		ModelName:   "filtered-model",
		Status:      model.AttemptSkipped,
		AttemptKind: "decision_only",
	}
	attempts := []model.ChannelAttempt{
		decisionOnly,
		{ChannelID: 20, ChannelName: "selected-provider", ModelName: "selected-model", Status: model.AttemptSuccess},
	}

	id, name := finalChannel(attempts)
	if id != 20 || name != "selected-provider" {
		t.Fatalf("final channel changed by decision-only envelope: id=%d name=%q", id, name)
	}
	if got := finalModel("", "requested-model", attempts); got != "selected-model" {
		t.Fatalf("final model changed by decision-only envelope: %q", got)
	}
}

func TestOnlyDecisionOnlyAttemptResolvesNoFinalProvider(t *testing.T) {
	attempts := []model.ChannelAttempt{{
		ChannelID: 99, ChannelName: "filtered-provider", ModelName: "filtered-model",
		Status: model.AttemptSkipped, AttemptKind: "decision_only",
	}}
	id, name := finalChannel(attempts)
	if id != 0 || name != "" {
		t.Fatalf("decision-only envelope must not become final provider: id=%d name=%q", id, name)
	}
	if got := finalModel("", "requested-model", attempts); got != "requested-model" {
		t.Fatalf("decision-only envelope must not become final model: %q", got)
	}
}
