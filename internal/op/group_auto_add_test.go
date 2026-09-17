package op

import (
	"context"
	"path/filepath"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
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

func setupGroupAutoAddTestDB(t *testing.T) context.Context {
	t.Helper()

	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	groupCache.Clear()
	groupMap.Clear()
	channelCache.Clear()

	dbPath := filepath.Join(t.TempDir(), "group-auto-add-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
		channelCache.Clear()
		_ = dbpkg.Close()
	})
	return context.Background()
}

func TestGroupAutoAddPersistsAndIsIdempotent(t *testing.T) {
	ctx := setupGroupAutoAddTestDB(t)
	group := &model.Group{Name: "claude", Mode: model.GroupModeRoundRobin}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	channelCache.Set(1, model.Channel{
		ID:      1,
		Name:    "provider",
		Enabled: true,
		Model:   "claude-3-5-sonnet,claude-3-5-haiku,gpt-5",
	})

	result, err := GroupAutoAdd(group.ID, ctx)
	if err != nil {
		t.Fatalf("first auto add: %v", err)
	}
	if result.Matched != 2 || result.Added != 2 {
		t.Fatalf("first result=%+v, want matched=2 added=2", result)
	}
	updated, err := GroupGet(group.ID, ctx)
	if err != nil {
		t.Fatalf("get updated group: %v", err)
	}
	if len(updated.Items) != 2 {
		t.Fatalf("items=%d, want 2", len(updated.Items))
	}
	if updated.Items[0].Priority != 1 || updated.Items[1].Priority != 2 {
		t.Fatalf("priorities=%d,%d, want 1,2", updated.Items[0].Priority, updated.Items[1].Priority)
	}

	second, err := GroupAutoAdd(group.ID, ctx)
	if err != nil {
		t.Fatalf("second auto add: %v", err)
	}
	if second.Matched != 2 || second.Added != 0 {
		t.Fatalf("second result=%+v, want matched=2 added=0", second)
	}
	updated, err = GroupGet(group.ID, ctx)
	if err != nil {
		t.Fatalf("get group after second click: %v", err)
	}
	if len(updated.Items) != 2 {
		t.Fatalf("items after second click=%d, want 2", len(updated.Items))
	}
}

func TestGroupAutoAddSyncsActivePreset(t *testing.T) {
	ctx := setupGroupAutoAddTestDB(t)
	group := &model.Group{Name: "claude", Mode: model.GroupModeRoundRobin}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	channelCache.Set(1, model.Channel{
		ID:      1,
		Name:    "provider",
		Enabled: true,
		Model:   "claude-3-5-sonnet,claude-3-5-haiku",
	})

	if _, err := GroupUpdate(&model.GroupUpdateRequest{
		ID: group.ID,
		ItemsToAdd: []model.GroupItemAddRequest{{
			ChannelID: 1,
			ModelName: "claude-3-5-sonnet",
			Priority:  1,
			Weight:    1,
		}},
	}, ctx); err != nil {
		t.Fatalf("seed group item: %v", err)
	}
	preset, err := GroupPresetCreate(group.ID, "baseline", ctx)
	if err != nil {
		t.Fatalf("create preset: %v", err)
	}
	if err := GroupPresetActivate(preset.ID, ctx); err != nil {
		t.Fatalf("activate preset: %v", err)
	}

	result, err := GroupAutoAdd(group.ID, ctx)
	if err != nil {
		t.Fatalf("auto add with active preset: %v", err)
	}
	if result.Matched != 2 || result.Added != 1 {
		t.Fatalf("result=%+v, want matched=2 added=1", result)
	}

	var synced model.GroupPreset
	if err := dbpkg.GetDB().WithContext(ctx).First(&synced, preset.ID).Error; err != nil {
		t.Fatalf("reload preset: %v", err)
	}
	if len(synced.Items) != 2 {
		t.Fatalf("active preset items=%d, want 2", len(synced.Items))
	}
	if synced.Items[0].ModelName != "claude-3-5-sonnet" || synced.Items[1].ModelName != "claude-3-5-haiku" {
		t.Fatalf("active preset items=%+v, want live group members", synced.Items)
	}
}
