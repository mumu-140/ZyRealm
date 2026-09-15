package balancer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
)

func traceVersionTestIterator(t *testing.T) *Iterator {
	t.Helper()
	availability.Reset()
	t.Cleanup(availability.Reset)
	it := NewIterator(model.Group{
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{{
			ChannelID: 10,
			ModelName: "upstream-model",
			Priority:  1,
		}},
	}, 0, "request-model")
	if !it.Next() {
		t.Fatal("expected provider candidate")
	}
	return it
}

func TestAttemptsMarksNewTraceWithoutChangingAttemptAccounting(t *testing.T) {
	it := traceVersionTestIterator(t)
	it.Skip(10, 0, "provider-a", "test skip")

	attempts := it.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("attempt cardinality = %d, want 1", len(attempts))
	}
	if attempts[0].AttemptNum != 1 {
		t.Fatalf("AttemptNum = %d, want 1", attempts[0].AttemptNum)
	}
	if attempts[0].DecisionTraceVersion != model.RoutingDecisionTraceVersion {
		t.Fatalf("decision trace version = %q, want %q", attempts[0].DecisionTraceVersion, model.RoutingDecisionTraceVersion)
	}
	if len(attempts[0].DecisionEvents) != 0 {
		t.Fatalf("version marker must not synthesize decisions: %#v", attempts[0].DecisionEvents)
	}
}

func TestDecisionOnlyEnvelopeCarriesTraceVersion(t *testing.T) {
	it := traceVersionTestIterator(t)
	it.RecordDecision(model.RoutingDecisionEvent{
		Stage:     model.DecisionStageCandidate,
		Outcome:   model.DecisionOutcomeRejected,
		Reason:    model.DecisionReasonRuntimeCooldown,
		ChannelID: 10,
	})

	attempts := it.Attempts()
	if len(attempts) != 1 || attempts[0].AttemptKind != "decision_only" {
		t.Fatalf("decision-only envelope = %#v", attempts)
	}
	if attempts[0].AttemptNum != 0 {
		t.Fatalf("decision-only AttemptNum = %d, want 0", attempts[0].AttemptNum)
	}
	if attempts[0].DecisionTraceVersion != model.RoutingDecisionTraceVersion {
		t.Fatalf("decision-only trace version = %q, want %q", attempts[0].DecisionTraceVersion, model.RoutingDecisionTraceVersion)
	}
}

func TestLegacyAttemptRoutingTraceOmitsVersionMarker(t *testing.T) {
	encoded, err := json.Marshal(model.AttemptRoutingTrace{})
	if err != nil {
		t.Fatalf("marshal legacy trace: %v", err)
	}
	if strings.Contains(string(encoded), "decision_trace_version") {
		t.Fatalf("zero-value historical trace must omit version marker: %s", encoded)
	}
}
