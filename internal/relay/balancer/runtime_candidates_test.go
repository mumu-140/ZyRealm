package balancer

import (
	"fmt"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
)

func TestRuntimeEligibilitySharedAcrossAllBalancerModes(t *testing.T) {
	modes := []model.GroupMode{
		model.GroupModeRoundRobin,
		model.GroupModeRandom,
		model.GroupModeFailover,
		model.GroupModeWeighted,
		model.GroupModeHealthFirst,
		model.GroupModeLeastUsed,
		model.GroupModeP2C,
		model.GroupModeStrictRandom,
	}

	for _, mode := range modes {
		t.Run(fmt.Sprintf("mode_%d", mode), func(t *testing.T) {
			availability.Reset()
			availability.RecordModelFailure(10, "upstream-a", "first_token_timeout", time.Now())
			group := model.Group{
				Mode: mode,
				Items: []model.GroupItem{
					{ChannelID: 10, ModelName: "upstream-a", Priority: 1, Weight: 100},
					{ChannelID: 20, ModelName: "upstream-b", Priority: 2, Weight: 1},
				},
			}
			it := NewIterator(group, 0, "request-model")
			if it.Len() != 1 {
				t.Fatalf("eligible candidates = %d, want 1", it.Len())
			}
			if !it.Next() {
				t.Fatalf("expected healthy candidate")
			}
			if got := it.Item().ChannelID; got != 20 {
				t.Fatalf("selected channel = %d, want 20; cooldown must be shared across mode %v", got, mode)
			}
		})
	}
}

func TestRuntimeSuspectFallsBehindAvailableRegardlessOfPriority(t *testing.T) {
	availability.Reset()
	availability.RecordModelSuspect(10, "upstream-a", "ambiguous_cancel", time.Now())
	group := model.Group{
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: 10, ModelName: "upstream-a", Priority: 1},
			{ChannelID: 20, ModelName: "upstream-b", Priority: 2},
		},
	}
	it := NewIterator(group, 0, "request-model")
	if !it.Next() {
		t.Fatalf("expected available candidate")
	}
	if got := it.Item().ChannelID; got != 20 {
		t.Fatalf("first channel = %d, want AVAILABLE channel 20 before SUSPECT priority-1 channel", got)
	}
	if !it.Next() || it.Item().ChannelID != 10 {
		t.Fatalf("expected suspect channel to remain as fallback")
	}
}

func TestRuntimeHalfOpenReentersConfiguredStrategy(t *testing.T) {
	availability.Reset()
	now := time.Now()
	availability.RecordProviderFailure(10, "upstream_503", now.Add(-10*time.Second))
	group := model.Group{
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: 10, ModelName: "upstream-a", Priority: 1},
			{ChannelID: 20, ModelName: "upstream-b", Priority: 2},
		},
	}
	it := NewIterator(group, 0, "request-model")
	if !it.Next() {
		t.Fatalf("expected expired provider to reenter candidate set as half-open")
	}
	if got := it.Item().ChannelID; got != 10 {
		t.Fatalf("first channel = %d, want priority-1 half-open channel 10 under Failover", got)
	}
}

func TestStickyCannotPromoteSuspectCandidate(t *testing.T) {
	availability.Reset()
	availability.RecordModelSuspect(10, "upstream-a", "ambiguous_cancel", time.Now())
	group := model.Group{
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: 10, ModelName: "upstream-a", Priority: 1},
			{ChannelID: 20, ModelName: "upstream-b", Priority: 2},
		},
	}
	it := NewIteratorWithPreference(group, 0, "request-model", &SessionEntry{ChannelID: 10, ChannelKeyID: 101})
	if !it.Next() {
		t.Fatalf("expected available candidate")
	}
	if got := it.Item().ChannelID; got != 20 {
		t.Fatalf("first channel = %d, want available channel 20", got)
	}
	if it.IsSticky() {
		t.Fatalf("suspect sticky candidate must not be promoted")
	}
}

func TestStickyCanPromoteAvailableCandidate(t *testing.T) {
	availability.Reset()
	group := model.Group{
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: 10, ModelName: "upstream-a", Priority: 1},
			{ChannelID: 20, ModelName: "upstream-b", Priority: 2},
		},
	}
	it := NewIteratorWithPreference(group, 0, "request-model", &SessionEntry{ChannelID: 20, ChannelKeyID: 202})
	if !it.Next() {
		t.Fatalf("expected sticky candidate")
	}
	if got := it.Item().ChannelID; got != 20 {
		t.Fatalf("first channel = %d, want sticky available channel 20", got)
	}
	if !it.IsSticky() || it.StickyKeyID() != 202 {
		t.Fatalf("available sticky preference was not preserved")
	}
}
