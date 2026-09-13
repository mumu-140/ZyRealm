package balancer

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
)

func TestResetStateByChannelClearsSharedAvailability(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	availability.RecordProviderFailure(101, "provider failure", now)
	if got := availability.CandidateState(101, "model-a", now); got != availability.StateCooldown {
		t.Fatalf("precondition state=%v, want cooldown", got)
	}

	ResetStateByChannel(101)
	if got := availability.CandidateState(101, "model-a", now); got != availability.StateAvailable {
		t.Fatalf("state after channel reset=%v, want available", got)
	}
}
