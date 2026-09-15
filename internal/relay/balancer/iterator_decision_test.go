package balancer

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
)

func decisionTestGroup() model.Group {
	return model.Group{
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: 10, ModelName: "upstream-a", Priority: 1},
			{ChannelID: 20, ModelName: "upstream-b", Priority: 2},
		},
	}
}

func TestRecordDecisionDoesNotChangeAttemptNumberOrCandidateOrder(t *testing.T) {
	availability.Reset()
	it := NewIterator(decisionTestGroup(), 0, "request-model")
	if got := it.Len(); got != 2 {
		t.Fatalf("Len before decision = %d, want 2", got)
	}

	it.RecordDecision(model.RoutingDecisionEvent{
		Stage:     model.DecisionStageCandidate,
		Outcome:   model.DecisionOutcomeEligible,
		ChannelID: 20,
	})

	if got := it.Len(); got != 2 {
		t.Fatalf("Len after decision = %d, want 2", got)
	}
	if !it.Next() || it.Item().ChannelID != 10 {
		t.Fatalf("first candidate changed after recording decision")
	}
	it.Skip(10, 0, "provider-a", "test skip")

	attempts := it.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("attempt cardinality = %d, want 1", len(attempts))
	}
	if attempts[0].AttemptNum != 1 {
		t.Fatalf("AttemptNum = %d, want 1; decision events must not increment iterator count", attempts[0].AttemptNum)
	}
	if len(attempts[0].DecisionEvents) != 1 {
		t.Fatalf("nested decision events = %d, want 1", len(attempts[0].DecisionEvents))
	}
	if attempts[0].DecisionEvents[0].Sequence != 1 {
		t.Fatalf("decision sequence = %d, want 1", attempts[0].DecisionEvents[0].Sequence)
	}
	if !it.Next() || it.Item().ChannelID != 20 {
		t.Fatalf("second candidate changed after recording decision")
	}
}

func TestRecordDecisionPreservesStickyAndSkippedProviderState(t *testing.T) {
	availability.Reset()
	it := NewIteratorWithPreference(
		decisionTestGroup(),
		0,
		"request-model",
		&SessionEntry{ChannelID: 20, ChannelKeyID: 202},
	)
	it.RecordDecision(model.RoutingDecisionEvent{
		Stage:     model.DecisionStageCandidate,
		Outcome:   model.DecisionOutcomeEligible,
		ChannelID: 20,
	})
	if !it.Next() || it.Item().ChannelID != 20 || !it.IsSticky() || it.StickyKeyID() != 202 {
		t.Fatalf("recording decision changed sticky selection")
	}

	it2 := NewIterator(decisionTestGroup(), 0, "request-model")
	it2.SkipProvider(10)
	it2.RecordDecision(model.RoutingDecisionEvent{
		Stage:     model.DecisionStageCandidate,
		Outcome:   model.DecisionOutcomeRejected,
		ChannelID: 10,
	})
	if !it2.Next() || it2.Item().ChannelID != 20 {
		t.Fatalf("recording decision changed request-local skipped-provider state")
	}
}

func TestAttemptsMaterializesDecisionLedgerExactlyOnceAndIdempotently(t *testing.T) {
	availability.Reset()
	it := NewIterator(decisionTestGroup(), 0, "request-model")
	if !it.Next() {
		t.Fatal("expected first candidate")
	}
	it.Skip(10, 0, "provider-a", "test skip")
	it.RecordDecision(model.RoutingDecisionEvent{
		Stage:     model.DecisionStageCandidate,
		Outcome:   model.DecisionOutcomeRejected,
		Reason:    model.DecisionReasonRuntimeCooldown,
		ChannelID: 10,
		Detail:    "cooldown active",
	})
	it.RecordDecision(model.RoutingDecisionEvent{
		Stage:     model.DecisionStageCandidate,
		Outcome:   model.DecisionOutcomeSelected,
		ChannelID: 20,
	})

	first := it.Attempts()
	second := it.Attempts()
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("Attempts cardinality changed: first=%d second=%d", len(first), len(second))
	}
	if first[0].AttemptNum != 1 || second[0].AttemptNum != 1 {
		t.Fatalf("AttemptNum changed while materializing decision ledger")
	}
	if len(first[0].DecisionEvents) != 2 || len(second[0].DecisionEvents) != 2 {
		t.Fatalf("decision ledger duplicated across Attempts calls: first=%d second=%d",
			len(first[0].DecisionEvents), len(second[0].DecisionEvents))
	}
	if first[0].DecisionEvents[0].Sequence != 1 || first[0].DecisionEvents[1].Sequence != 2 {
		t.Fatalf("unexpected decision sequence: %#v", first[0].DecisionEvents)
	}

	first[0].DecisionEvents[0].Detail = "mutated outside iterator"
	third := it.Attempts()
	if got := third[0].DecisionEvents[0].Detail; got != "cooldown active" {
		t.Fatalf("returned Attempts snapshot mutated iterator ledger: %q", got)
	}
}

func TestAttemptsEmptyWithoutAttemptsOrDecisionEvents(t *testing.T) {
	availability.Reset()
	it := NewIterator(decisionTestGroup(), 0, "request-model")
	if got := len(it.Attempts()); got != 0 {
		t.Fatalf("empty iterator Attempts = %d, want 0", got)
	}
}

func TestAttemptsUsesDecisionOnlyEnvelopeWithoutConsumingAttemptNumber(t *testing.T) {
	availability.Reset()
	it := NewIterator(decisionTestGroup(), 0, "request-model")
	it.RecordDecision(model.RoutingDecisionEvent{
		Stage:       model.DecisionStageCandidate,
		Outcome:     model.DecisionOutcomeRejected,
		Reason:      model.DecisionReasonRuntimeCooldown,
		ChannelID:   10,
		ChannelName: "provider-a",
		ModelName:   "upstream-a",
	})

	attempts := it.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("decision-only cardinality = %d, want 1", len(attempts))
	}
	got := attempts[0]
	if got.Status != model.AttemptSkipped {
		t.Fatalf("decision-only status = %q, want %q", got.Status, model.AttemptSkipped)
	}
	if got.AttemptKind != "decision_only" {
		t.Fatalf("decision-only kind = %q, want decision_only", got.AttemptKind)
	}
	if got.AttemptNum != 0 {
		t.Fatalf("decision-only AttemptNum = %d, want 0", got.AttemptNum)
	}
	if len(got.DecisionEvents) != 1 || got.DecisionEvents[0].Sequence != 1 {
		t.Fatalf("decision-only ledger = %#v", got.DecisionEvents)
	}

	if !it.Next() {
		t.Fatal("expected scheduler to remain untouched")
	}
	it.Skip(it.Item().ChannelID, 0, "provider-a", "real scheduler skip")
	after := it.Attempts()
	if len(after) != 1 || after[0].AttemptNum != 1 {
		t.Fatalf("decision-only materialization consumed scheduler attempt number: %#v", after)
	}
}
