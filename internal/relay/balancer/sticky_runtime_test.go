package balancer

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
)

func TestStickyCannotRestoreCooldownCandidate(t *testing.T) {
	availability.Reset()
	defer availability.Reset()

	availability.RecordProviderFailure(10, "upstream_503", time.Now())
	group := model.Group{
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: 10, ModelName: "upstream-a", Priority: 1, Weight: 1},
			{ChannelID: 20, ModelName: "upstream-b", Priority: 2, Weight: 1},
		},
	}

	it := NewIteratorWithPreference(group, 0, "request-model", &SessionEntry{
		ChannelID:    10,
		ChannelKeyID: 101,
	})
	if it.Len() != 1 {
		t.Fatalf("eligible candidate count = %d, want 1 after cooling sticky provider is filtered", it.Len())
	}
	if !it.Next() {
		t.Fatalf("expected healthy fallback candidate")
	}
	if got := it.Item().ChannelID; got != 20 {
		t.Fatalf("selected channel = %d, want healthy fallback 20", got)
	}
	if it.IsSticky() || it.StickyKeyID() != 0 {
		t.Fatalf("cooldown provider must not retain sticky routing status")
	}
	if it.Next() {
		t.Fatalf("cooldown sticky provider must not reappear later in the iterator")
	}
}
