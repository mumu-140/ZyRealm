package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestResolveGroupAutoAddFuzzyMatchingIsCaseInsensitiveAndDeterministic(t *testing.T) {
	group := model.Group{Name: "claude-3-5"}
	llms := []model.LLMChannel{
		{Name: "CLAUDE-3-5-HAIKU", ChannelID: 2, ChannelName: "c2", Enabled: true},
		{Name: "gpt-5", ChannelID: 3, ChannelName: "c3", Enabled: true},
		{Name: "claude-3-5-sonnet", ChannelID: 1, ChannelName: "c1", Enabled: true},
	}

	adds, matched, err := resolveGroupAutoAddCandidates(group, llms)
	if err != nil {
		t.Fatalf("resolve candidates: %v", err)
	}
	if matched != 2 {
		t.Fatalf("matched=%d, want 2", matched)
	}
	if len(adds) != 2 {
		t.Fatalf("adds=%d, want 2", len(adds))
	}
	if adds[0].ModelName != "CLAUDE-3-5-HAIKU" || adds[0].ChannelID != 2 {
		t.Fatalf("first add=%+v, want HAIKU/channel 2", adds[0])
	}
	if adds[1].ModelName != "claude-3-5-sonnet" || adds[1].ChannelID != 1 {
		t.Fatalf("second add=%+v, want sonnet/channel 1", adds[1])
	}
}

func TestResolveGroupAutoAddRegexTakesPrecedenceOverGroupName(t *testing.T) {
	group := model.Group{
		Name:       "gpt",
		MatchRegex: `(?i)^claude-.*-sonnet$`,
	}
	llms := []model.LLMChannel{
		{Name: "gpt-5", ChannelID: 1},
		{Name: "claude-3-5-haiku", ChannelID: 2},
		{Name: "claude-3-5-sonnet", ChannelID: 3},
	}

	adds, matched, err := resolveGroupAutoAddCandidates(group, llms)
	if err != nil {
		t.Fatalf("resolve candidates: %v", err)
	}
	if matched != 1 || len(adds) != 1 {
		t.Fatalf("matched=%d adds=%d, want 1/1", matched, len(adds))
	}
	if adds[0].ModelName != "claude-3-5-sonnet" {
		t.Fatalf("model=%q, want claude-3-5-sonnet", adds[0].ModelName)
	}
}

func TestResolveGroupAutoAddExcludesExistingMembersAndAppendsPriority(t *testing.T) {
	group := model.Group{
		Name: "claude",
		Items: []model.GroupItem{
			{ChannelID: 2, ModelName: "claude-3-5-sonnet", Priority: 4, Weight: 7},
			{ChannelID: 9, ModelName: "other", Priority: 8, Weight: 1},
		},
	}
	llms := []model.LLMChannel{
		{Name: "claude-3-5-sonnet", ChannelID: 2},
		{Name: "claude-3-opus", ChannelID: 5},
		{Name: "claude-3-haiku", ChannelID: 4},
	}

	adds, matched, err := resolveGroupAutoAddCandidates(group, llms)
	if err != nil {
		t.Fatalf("resolve candidates: %v", err)
	}
	if matched != 3 {
		t.Fatalf("matched=%d, want 3", matched)
	}
	if len(adds) != 2 {
		t.Fatalf("adds=%d, want 2", len(adds))
	}
	if adds[0].ModelName != "claude-3-haiku" || adds[0].Priority != 9 || adds[0].Weight != 1 {
		t.Fatalf("first add=%+v, want haiku priority 9 weight 1", adds[0])
	}
	if adds[1].ModelName != "claude-3-opus" || adds[1].Priority != 10 || adds[1].Weight != 1 {
		t.Fatalf("second add=%+v, want opus priority 10 weight 1", adds[1])
	}
}

func TestResolveGroupAutoAddInvalidRegexReturnsErrorWithoutFallback(t *testing.T) {
	group := model.Group{Name: "gpt", MatchRegex: "["}
	llms := []model.LLMChannel{{Name: "gpt-5", ChannelID: 1}}

	adds, matched, err := resolveGroupAutoAddCandidates(group, llms)
	if err == nil {
		t.Fatal("expected invalid regex error")
	}
	if matched != 0 {
		t.Fatalf("matched=%d, want 0", matched)
	}
	if len(adds) != 0 {
		t.Fatalf("adds=%d, want 0", len(adds))
	}
}

func TestResolveGroupAutoAddDoesNotMutateInputOrdering(t *testing.T) {
	group := model.Group{Name: "m"}
	llms := []model.LLMChannel{
		{Name: "m-z", ChannelID: 7},
		{Name: "m-a", ChannelID: 2},
	}

	_, _, _ = resolveGroupAutoAddCandidates(group, llms)

	if llms[0].Name != "m-z" || llms[1].Name != "m-a" {
		t.Fatalf("input slice mutated: %+v", llms)
	}
}
