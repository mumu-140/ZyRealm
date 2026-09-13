package balancer

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestIteratorSkipProviderSkipsRemainingEntriesForChannel(t *testing.T) {
	Reset()
	group := model.Group{
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: 10, ModelName: "model-a", Priority: 1},
			{ChannelID: 10, ModelName: "model-b", Priority: 2},
			{ChannelID: 20, ModelName: "model-c", Priority: 3},
		},
	}

	it := NewIterator(group, 0, "request-model")
	if !it.Next() {
		t.Fatalf("expected first provider candidate")
	}
	if got := it.Item().ChannelID; got != 10 {
		t.Fatalf("first channel = %d, want 10", got)
	}

	it.SkipProvider(10)
	if !it.Next() {
		t.Fatalf("expected iterator to advance to next provider")
	}
	if got := it.Item().ChannelID; got != 20 {
		t.Fatalf("channel after provider skip = %d, want 20", got)
	}
	if it.Next() {
		t.Fatalf("expected all remaining channel 10 entries to stay skipped")
	}
}
