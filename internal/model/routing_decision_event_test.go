package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRoutingDecisionEventContractValues(t *testing.T) {
	if got := string(DecisionStageCandidate); got != "candidate" {
		t.Fatalf("candidate stage = %q, want candidate", got)
	}
	if got := string(DecisionStageCredential); got != "credential" {
		t.Fatalf("credential stage = %q, want credential", got)
	}
	if got := string(DecisionStageProtocol); got != "protocol" {
		t.Fatalf("protocol stage = %q, want protocol", got)
	}
	if got := string(DecisionStageDispatch); got != "dispatch" {
		t.Fatalf("dispatch stage = %q, want dispatch", got)
	}
	if got := string(DecisionStageFailover); got != "failover" {
		t.Fatalf("failover stage = %q, want failover", got)
	}

	if got := string(DecisionOutcomeEligible); got != "eligible" {
		t.Fatalf("eligible outcome = %q, want eligible", got)
	}
	if got := string(DecisionOutcomeRejected); got != "rejected" {
		t.Fatalf("rejected outcome = %q, want rejected", got)
	}
	if got := string(DecisionOutcomeSelected); got != "selected" {
		t.Fatalf("selected outcome = %q, want selected", got)
	}
	if got := string(DecisionOutcomeAttempted); got != "attempted" {
		t.Fatalf("attempted outcome = %q, want attempted", got)
	}
	if got := string(DecisionOutcomeStopped); got != "stopped" {
		t.Fatalf("stopped outcome = %q, want stopped", got)
	}

	reasons := map[RoutingDecisionReason]string{
		DecisionReasonRuntimeCooldown:    "runtime_cooldown",
		DecisionReasonRuntimeSuspect:     "runtime_suspect",
		DecisionReasonCredentialCooldown: "credential_cooldown",
		DecisionReasonCapabilityNegative: "capability_negative",
		DecisionReasonCircuitBreak:       "circuit_break",
		DecisionReasonCapacity:           "capacity",
		DecisionReasonRateLimit:          "rate_limit",
		DecisionReasonProtocolIncompatible: "protocol_incompatible",
		DecisionReasonAttemptBudget:      "attempt_budget",
		DecisionReasonReplayUnsafe:       "replay_unsafe",
	}
	for reason, want := range reasons {
		if got := string(reason); got != want {
			t.Fatalf("reason %q = %q, want %q", reason, got, want)
		}
	}
}

func TestRoutingDecisionEventJSONRoundTripPreservesOrder(t *testing.T) {
	trace := AttemptRoutingTrace{
		DecisionEvents: []RoutingDecisionEvent{
			{
				Sequence:     1,
				Stage:        DecisionStageCandidate,
				Outcome:      DecisionOutcomeRejected,
				Reason:       DecisionReasonRuntimeCooldown,
				ChannelID:    12,
				ChannelName:  "provider-a",
				ModelName:    "gpt-test",
				Protocol:     "responses",
				ExpiresAt:    12345,
				Detail:       "cooldown active",
			},
			{
				Sequence:     2,
				Stage:        DecisionStageCredential,
				Outcome:      DecisionOutcomeRejected,
				Reason:       DecisionReasonCredentialCooldown,
				ChannelID:    13,
				ChannelKeyID: 99,
				ChannelName:  "provider-b",
				ModelName:    "gpt-test",
			},
		},
	}

	encoded, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("marshal trace: %v", err)
	}

	var decoded AttemptRoutingTrace
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal trace: %v", err)
	}
	if !reflect.DeepEqual(decoded.DecisionEvents, trace.DecisionEvents) {
		t.Fatalf("decision event round trip mismatch:\n got: %#v\nwant: %#v", decoded.DecisionEvents, trace.DecisionEvents)
	}
}

func TestAttemptRoutingTraceOmitsEmptyDecisionEvents(t *testing.T) {
	encoded, err := json.Marshal(AttemptRoutingTrace{})
	if err != nil {
		t.Fatalf("marshal empty trace: %v", err)
	}
	if strings.Contains(string(encoded), "decision_events") {
		t.Fatalf("empty trace must omit decision_events: %s", encoded)
	}
}

func TestRoutingDecisionEventModelDoesNotExposeSensitiveFields(t *testing.T) {
	typeOf := reflect.TypeOf(RoutingDecisionEvent{})
	forbidden := map[string]struct{}{
		"credential":       {},
		"credential_value": {},
		"api_key":          {},
		"authorization":    {},
		"headers":          {},
		"request":          {},
		"response":         {},
		"prompt":           {},
		"content":          {},
	}
	for i := 0; i < typeOf.NumField(); i++ {
		field := typeOf.Field(i)
		jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
		jsonName = strings.ToLower(strings.TrimSpace(jsonName))
		if _, blocked := forbidden[jsonName]; blocked {
			t.Fatalf("RoutingDecisionEvent exposes forbidden JSON field %q", jsonName)
		}
	}
}
