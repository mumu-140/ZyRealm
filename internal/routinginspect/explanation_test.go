package routinginspect

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestBuildExplanationUsesOnlyPersistedHistoricalTrace(t *testing.T) {
	logItem := model.RelayLog{
		ID:               123,
		Time:             456,
		RequestModelName: "requested-model",
		Success:          true,
		RequestContent:   "SECRET_PROMPT_MUST_NOT_ESCAPE",
		ResponseContent:  "SECRET_RESPONSE_MUST_NOT_ESCAPE",
		Error:            "SECRET_ERROR_MUST_NOT_ESCAPE",
		Attempts: []model.ChannelAttempt{
			{
				ChannelID: 10, ChannelKeyID: 101, ChannelName: "provider-a", ModelName: "upstream-a",
				AttemptNum: 1, Status: model.AttemptFailed, Duration: 100, Msg: "SECRET_ATTEMPT_MESSAGE",
				SelectedProtocol: "openai_chat",
				AttemptRoutingTrace: model.AttemptRoutingTrace{
					DecisionTraceVersion: model.RoutingDecisionTraceVersion,
					FailureDomain:        "provider",
					FailureScope:         "provider",
					RuleID:               "provider_5xx",
					RetryDirective:       "next_provider",
					RuntimeEffect:        "provider_failure",
					RuntimeState:         "cooldown",
					CooldownUntil:        999,
					ReplaySafety:         "safe",
					DispatchState:        "sent_no_response",
					ProviderAttempt:      1,
					WireAttempt:          1,
					DecisionEvents: []model.RoutingDecisionEvent{{
						Sequence: 2, Stage: model.DecisionStageCredential, Outcome: model.DecisionOutcomeRejected,
						Reason: model.DecisionReasonCredentialCooldown, ChannelID: 10, ChannelKeyID: 100,
						Detail: "SECRET_DECISION_DETAIL_MUST_NOT_ESCAPE",
					}},
				},
			},
			{
				ChannelID: 20, ChannelKeyID: 201, ChannelName: "provider-b", ModelName: "upstream-b",
				AttemptNum: 2, Status: model.AttemptSuccess, Duration: 200, SelectedProtocol: "openai_responses",
				AttemptRoutingTrace: model.AttemptRoutingTrace{
					DecisionTraceVersion: model.RoutingDecisionTraceVersion,
					DispatchState:        "response_started",
					ProviderAttempt:      2,
					WireAttempt:          2,
					DecisionEvents: []model.RoutingDecisionEvent{{
						Sequence: 1, Stage: model.DecisionStageCandidate, Outcome: model.DecisionOutcomeRejected,
						Reason: model.DecisionReasonRuntimeCooldown, ChannelID: 30, ModelName: "upstream-c",
					}},
				},
			},
		},
	}

	got := BuildExplanation(logItem)
	if got.Version != ExplanationVersion {
		t.Fatalf("version = %q, want %q", got.Version, ExplanationVersion)
	}
	if got.Completeness != CompletenessComplete {
		t.Fatalf("completeness = %q, want %q", got.Completeness, CompletenessComplete)
	}
	if got.LogID != 123 || got.Time != 456 || got.RequestedModel != "requested-model" || !got.Success {
		t.Fatalf("identity summary = %#v", got)
	}
	if got.FinalRoute == nil || got.FinalRoute.ChannelID != 20 || got.FinalRoute.ChannelKeyID != 201 || got.FinalRoute.ModelName != "upstream-b" {
		t.Fatalf("final route = %#v", got.FinalRoute)
	}
	if got.FinalRoute.Status != model.AttemptSuccess || got.FinalRoute.Protocol != "openai_responses" {
		t.Fatalf("final route result = %#v", got.FinalRoute)
	}
	if len(got.Decisions) != 2 || got.Decisions[0].Sequence != 1 || got.Decisions[1].Sequence != 2 {
		t.Fatalf("decisions are not globally sequence ordered: %#v", got.Decisions)
	}
	if got.Decisions[1].Detail != "" {
		t.Fatalf("explanation must strip free-form decision detail: %#v", got.Decisions[1])
	}
	if len(got.Attempts) != 2 || got.Attempts[0].AttemptNum != 1 || got.Attempts[1].AttemptNum != 2 {
		t.Fatalf("attempt summaries = %#v", got.Attempts)
	}
	if got.Attempts[0].FailureDomain != "provider" || got.Attempts[0].RuntimeState != "cooldown" || got.Attempts[0].WireAttempt != 1 {
		t.Fatalf("attempt trace summary lost persisted facts: %#v", got.Attempts[0])
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal explanation: %v", err)
	}
	for _, secret := range []string{
		"SECRET_PROMPT_MUST_NOT_ESCAPE",
		"SECRET_RESPONSE_MUST_NOT_ESCAPE",
		"SECRET_ERROR_MUST_NOT_ESCAPE",
		"SECRET_ATTEMPT_MESSAGE",
		"SECRET_DECISION_DETAIL_MUST_NOT_ESCAPE",
	} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("explanation leaked sensitive source field %q: %s", secret, encoded)
		}
	}
}

func TestBuildExplanationMarksLegacyLogPartialWithoutInventingReasons(t *testing.T) {
	logItem := model.RelayLog{
		ID:               200,
		RequestModelName: "requested-model",
		Success:          false,
		Attempts: []model.ChannelAttempt{{
			ChannelID: 11, ChannelKeyID: 12, ChannelName: "legacy-provider", ModelName: "legacy-model",
			AttemptNum: 1, Status: model.AttemptFailed, Duration: 99,
		}},
	}

	got := BuildExplanation(logItem)
	if got.Completeness != CompletenessLegacyPartial {
		t.Fatalf("legacy completeness = %q", got.Completeness)
	}
	if len(got.Decisions) != 0 {
		t.Fatalf("legacy log must not invent decision reasons: %#v", got.Decisions)
	}
	if got.FinalRoute == nil || got.FinalRoute.ChannelID != 11 || got.FinalRoute.Status != model.AttemptFailed {
		t.Fatalf("legacy final route = %#v", got.FinalRoute)
	}
}

func TestBuildExplanationMarksDecisionOnlyAndHasNoFinalRoute(t *testing.T) {
	logItem := model.RelayLog{
		ID:               300,
		RequestModelName: "requested-model",
		Success:          false,
		Attempts: []model.ChannelAttempt{{
			ChannelID: 30, ChannelName: "filtered-provider", ModelName: "filtered-model",
			AttemptNum: 0, Status: model.AttemptSkipped, AttemptKind: "decision_only",
			AttemptRoutingTrace: model.AttemptRoutingTrace{
				DecisionTraceVersion: model.RoutingDecisionTraceVersion,
				DecisionEvents: []model.RoutingDecisionEvent{{
					Sequence: 1, Stage: model.DecisionStageCandidate, Outcome: model.DecisionOutcomeRejected,
					Reason: model.DecisionReasonRuntimeCooldown, ChannelID: 30,
				}},
			},
		}},
	}

	got := BuildExplanation(logItem)
	if got.Completeness != CompletenessDecisionOnly {
		t.Fatalf("decision-only completeness = %q", got.Completeness)
	}
	if got.FinalRoute != nil {
		t.Fatalf("decision-only trace must not become a final route: %#v", got.FinalRoute)
	}
	if len(got.Decisions) != 1 || got.Decisions[0].Reason != model.DecisionReasonRuntimeCooldown {
		t.Fatalf("decision-only decisions = %#v", got.Decisions)
	}
}

func TestBuildExplanationFallsBackToLastRealFailureNotSkippedEnvelope(t *testing.T) {
	logItem := model.RelayLog{
		Attempts: []model.ChannelAttempt{
			{ChannelID: 10, ChannelName: "failed-provider", ModelName: "m1", AttemptNum: 1, Status: model.AttemptFailed},
			{ChannelID: 20, ChannelName: "skipped-provider", ModelName: "m2", AttemptNum: 2, Status: model.AttemptSkipped},
		},
	}
	got := BuildExplanation(logItem)
	if got.FinalRoute == nil || got.FinalRoute.ChannelID != 10 || got.FinalRoute.Status != model.AttemptFailed {
		t.Fatalf("final failure selection = %#v", got.FinalRoute)
	}
}
