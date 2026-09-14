package relay

import (
	"errors"
	"testing"
)

func TestRelayAttemptBudgetCountsUniqueProvidersAndWires(t *testing.T) {
	budget := newRelayAttemptBudgetWithLimits(2, 4)

	for _, channelID := range []int{10, 10, 20, 20} {
		if err := budget.tryStartWire(channelID); err != nil {
			t.Fatalf("tryStartWire(%d) unexpected error: %v", channelID, err)
		}
	}
	if len(budget.providers) != 2 {
		t.Fatalf("provider count = %d, want 2", len(budget.providers))
	}
	if budget.wires != 4 {
		t.Fatalf("wire count = %d, want 4", budget.wires)
	}
	if !budget.wireExhausted() {
		t.Fatalf("expected wire budget to be exhausted")
	}
	if err := budget.tryStartWire(10); !errors.Is(err, errRelayWireAttemptsExceeded) {
		t.Fatalf("wire overflow error = %v, want %v", err, errRelayWireAttemptsExceeded)
	}
}

func TestRelayAttemptBudgetRejectsNewProviderButAllowsSeenProvider(t *testing.T) {
	budget := newRelayAttemptBudgetWithLimits(2, 8)
	if err := budget.tryStartWire(10); err != nil {
		t.Fatal(err)
	}
	if err := budget.tryStartWire(20); err != nil {
		t.Fatal(err)
	}
	if budget.canUseProvider(30) {
		t.Fatalf("new provider should fail preflight after unique provider budget is full")
	}
	if !budget.canUseProvider(10) {
		t.Fatalf("already-seen provider should pass preflight while wire budget remains")
	}
	if err := budget.tryStartWire(30); !errors.Is(err, errRelayProviderAttemptsExceeded) {
		t.Fatalf("new provider overflow error = %v, want %v", err, errRelayProviderAttemptsExceeded)
	}
	if err := budget.tryStartWire(10); err != nil {
		t.Fatalf("seen provider should remain eligible within wire budget: %v", err)
	}
	if len(budget.providers) != 2 {
		t.Fatalf("provider count changed after rejected provider: %d", len(budget.providers))
	}
	if budget.wires != 3 {
		t.Fatalf("wire count = %d, want 3; rejected provider must not consume a wire", budget.wires)
	}
}

func TestRelayAttemptBudgetAllowsMoreThanFourProvidersWhenConfigured(t *testing.T) {
	budget := newRelayAttemptBudgetWithLimits(10, 20)
	for channelID := 1; channelID <= 10; channelID++ {
		if err := budget.tryStartWire(channelID); err != nil {
			t.Fatalf("provider %d should be allowed: %v", channelID, err)
		}
	}
	if len(budget.providers) != 10 {
		t.Fatalf("provider count = %d, want 10", len(budget.providers))
	}
	if budget.canUseProvider(11) {
		t.Fatalf("provider 11 should be rejected at configured provider limit")
	}
}

func TestRelayAttemptBudgetAllowsWireBudgetAboveTwenty(t *testing.T) {
	budget := newRelayAttemptBudgetWithLimits(100, 50)
	for attempt := 0; attempt < 50; attempt++ {
		if err := budget.tryStartWire(attempt + 1); err != nil {
			t.Fatalf("wire attempt %d should be allowed: %v", attempt+1, err)
		}
	}
	if budget.maxWires != 50 {
		t.Fatalf("maxWires = %d, want 50", budget.maxWires)
	}
	if err := budget.tryStartWire(51); !errors.Is(err, errRelayWireAttemptsExceeded) {
		t.Fatalf("wire overflow error = %v, want %v", err, errRelayWireAttemptsExceeded)
	}
}

func TestRelayAttemptBudgetLimitsUnknownCrossProviderReplay(t *testing.T) {
	budget := newRelayAttemptBudgetWithLimits(defaultMaxProviderAttempts, defaultMaxWireAttempts)
	if !budget.tryUnknownCrossProviderReplay() {
		t.Fatalf("first unknown cross-provider replay should be allowed")
	}
	if budget.tryUnknownCrossProviderReplay() {
		t.Fatalf("second unknown cross-provider replay should be rejected")
	}
	if budget.unknownReplayCount != 1 {
		t.Fatalf("unknownReplayCount = %d, want 1", budget.unknownReplayCount)
	}
}

func TestRelayAttemptBudgetUsesProductionDefaults(t *testing.T) {
	budget := newRelayAttemptBudgetWithLimits(defaultMaxProviderAttempts, defaultMaxWireAttempts)
	if budget.maxProviders != 20 {
		t.Fatalf("maxProviders = %d, want 20", budget.maxProviders)
	}
	if budget.maxWires != 20 {
		t.Fatalf("maxWires = %d, want 20", budget.maxWires)
	}
	if budget.maxUnknownReplays != 1 {
		t.Fatalf("maxUnknownReplays = %d, want 1", budget.maxUnknownReplays)
	}
}
