package balancer

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/outlierwindow"
)

func TestFailoverOrderingUsesPassiveHealthNotLegacyCircuit(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	now := time.Now()
	seedHealth(1801, 10, 0, now)
	seedHealth(1802, 0, 10, now)
	seedCircuitEntry(1801, 1, "m", circuitSeed{
		State:           StateOpen,
		LastFailureTime: now,
		TripCount:       1,
	})

	items := []model.GroupItem{
		mkItem(1, 1, 1801),
		mkItem(2, 1, 1802),
	}
	shared := (&Failover{}).Candidates(items)
	if shared[0].ChannelID != 1801 {
		t.Fatalf("shared first channel = %d, want healthy ch1801; legacy circuit must not influence shared ordering", shared[0].ChannelID)
	}

	ordered := runtimeOrderedCandidates(model.Group{Mode: model.GroupModeFailover, Items: items}, "m", now)
	if ordered[0].ChannelID != 1801 {
		t.Fatalf("runtime first channel = %d, want healthy ch1801", ordered[0].ChannelID)
	}
}

func TestHealthFirstOrderingUsesPassiveHealthNotLegacyCircuit(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	now := time.Now()
	seedHealth(1811, 10, 0, now)
	outlierwindow.ClearChannel(1812)
	seedCircuitEntry(1811, 1, "m", circuitSeed{
		State:           StateOpen,
		LastFailureTime: now,
		TripCount:       1,
	})

	items := []model.GroupItem{
		mkItem(1, 1, 1811),
		mkItem(2, 1, 1812),
	}
	shared := (&HealthFirst{}).Candidates(items)
	if shared[0].ChannelID != 1811 {
		t.Fatalf("shared first channel = %d, want healthy ch1811; legacy circuit must not influence health tiers", shared[0].ChannelID)
	}

	ordered := runtimeOrderedCandidates(model.Group{Mode: model.GroupModeHealthFirst, Items: items}, "m", now)
	if ordered[0].ChannelID != 1811 {
		t.Fatalf("runtime first channel = %d, want healthy ch1811", ordered[0].ChannelID)
	}
}
