package balancer

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
)

func runtimeDecisionGroup() model.Group {
	return model.Group{
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: 10, ModelName: "upstream-a", Priority: 1},
			{ChannelID: 20, ModelName: "upstream-b", Priority: 2},
		},
	}
}

func findDecisionEvent(attempts []model.ChannelAttempt, reason model.RoutingDecisionReason) (model.RoutingDecisionEvent, bool) {
	for _, attempt := range attempts {
		for _, event := range attempt.DecisionEvents {
			if event.Reason == reason {
				return event, true
			}
		}
	}
	return model.RoutingDecisionEvent{}, false
}

func TestRuntimeCooldownIsTracedWithoutChangingExecutableCandidates(t *testing.T) {
	availability.Reset()
	now := time.Now()
	cooldownUntil := availability.RecordModelFailure(10, "upstream-a", "first_token_timeout", now)

	it := NewIterator(runtimeDecisionGroup(), 0, "request-model")
	if got := it.Len(); got != 1 {
		t.Fatalf("eligible candidates = %d, want 1", got)
	}
	if !it.Next() || it.Item().ChannelID != 20 {
		t.Fatalf("runtime tracing changed selected candidate; want channel 20")
	}

	event, ok := findDecisionEvent(it.Attempts(), model.DecisionReasonRuntimeCooldown)
	if !ok {
		t.Fatal("missing runtime_cooldown decision event")
	}
	if event.Stage != model.DecisionStageCandidate || event.Outcome != model.DecisionOutcomeRejected {
		t.Fatalf("cooldown event = stage %q outcome %q", event.Stage, event.Outcome)
	}
	if event.ChannelID != 10 || event.ModelName != "upstream-a" {
		t.Fatalf("cooldown event identity = channel %d model %q", event.ChannelID, event.ModelName)
	}
	if event.ExpiresAt < cooldownUntil.Unix()-1 || event.ExpiresAt > cooldownUntil.Unix()+1 {
		t.Fatalf("cooldown expiry = %d, want around %d", event.ExpiresAt, cooldownUntil.Unix())
	}
	if event.Detail != "first_token_timeout" {
		t.Fatalf("cooldown detail = %q, want first_token_timeout", event.Detail)
	}
}

func TestRuntimeSuspectTraceDoesNotChangeFallbackTier(t *testing.T) {
	availability.Reset()
	availability.RecordModelSuspect(10, "upstream-a", "ambiguous_cancel", time.Now())

	it := NewIterator(runtimeDecisionGroup(), 0, "request-model")
	if !it.Next() || it.Item().ChannelID != 20 {
		t.Fatalf("available candidate must stay ahead of suspect candidate")
	}
	if !it.Next() || it.Item().ChannelID != 10 {
		t.Fatalf("suspect candidate must remain executable as fallback")
	}

	event, ok := findDecisionEvent(it.Attempts(), model.DecisionReasonRuntimeSuspect)
	if !ok {
		t.Fatal("missing runtime_suspect decision event")
	}
	if event.Stage != model.DecisionStageCandidate || event.Outcome != model.DecisionOutcomeEligible {
		t.Fatalf("suspect event = stage %q outcome %q", event.Stage, event.Outcome)
	}
	if event.ChannelID != 10 || event.Detail != "ambiguous_cancel" {
		t.Fatalf("suspect event = %#v", event)
	}
}

func TestRuntimeInstrumentationDoesNotConsumeHalfOpenLease(t *testing.T) {
	availability.Reset()
	now := time.Now()
	availability.RecordProviderFailure(10, "upstream_503", now.Add(-10*time.Second))

	it := NewIterator(runtimeDecisionGroup(), 0, "request-model")
	if !it.Next() || it.Item().ChannelID != 10 {
		t.Fatalf("half-open candidate must reenter configured failover order")
	}

	lease, ok := availability.AcquireCandidate(10, "upstream-a", time.Now())
	if !ok {
		t.Fatal("iterator construction consumed half-open recovery lease")
	}
	availability.ReleaseLease(lease, time.Now())
}

func TestRuntimeAvailableCandidatesRemainUnmodified(t *testing.T) {
	availability.Reset()
	it := NewIterator(runtimeDecisionGroup(), 0, "request-model")
	if got := it.Len(); got != 2 {
		t.Fatalf("eligible candidates = %d, want 2", got)
	}
	if !it.Next() || it.Item().ChannelID != 10 {
		t.Fatalf("first failover candidate changed")
	}
	if !it.Next() || it.Item().ChannelID != 20 {
		t.Fatalf("second failover candidate changed")
	}
	if got := len(it.Attempts()); got != 0 {
		t.Fatalf("available-only routing unexpectedly emitted decision envelope: %d", got)
	}
}
