package task

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func setupSyncModelFilterTest(t *testing.T) context.Context {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	if err := dbpkg.InitDB("sqlite", filepath.Join(t.TempDir(), "sync-model-filter.db"), false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("InitCache failed: %v", err)
	}
	t.Cleanup(func() { _ = dbpkg.Close() })
	return context.Background()
}

func syncModelListServer(t *testing.T, models ...string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]map[string]string, 0, len(models))
		for _, name := range models {
			data = append(data, map[string]string{"id": name})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	return server
}

func createAutoSyncChannelForModelFilterTest(t *testing.T, ctx context.Context, name, baseURL, models string) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Name:     name,
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		AutoSync: true,
		BaseUrls: []model.BaseUrl{{URL: baseURL}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
		Model:    models,
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	return channel
}

func TestSyncModelsTaskTreatsGlobalFilterExclusionsAsNormalRemovals(t *testing.T) {
	ctx := setupSyncModelFilterTest(t)
	if err := op.SettingSetString(model.SettingKeyModelFilterRegex, `^gpt-`); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}
	server := syncModelListServer(t, "gpt-4o", "claude-3-5-sonnet")
	channel := createAutoSyncChannelForModelFilterTest(t, ctx, "sync-filter-channel", server.URL, "gpt-4o,claude-3-5-sonnet")

	report := SyncModelsTaskWithReport()
	if report.Failed != 0 || report.Updated != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(report.Results) != 1 || len(report.Results[0].RemovedModels) != 1 || report.Results[0].RemovedModels[0] != "claude-3-5-sonnet" {
		t.Fatalf("expected filtered model removal, got %#v", report.Results)
	}
	updated, err := op.ChannelGet(channel.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet failed: %v", err)
	}
	if updated.Model != "gpt-4o" {
		t.Fatalf("persisted models=%q, want gpt-4o", updated.Model)
	}
}

func TestSyncModelsTaskInvalidGlobalFilterPreservesExistingModels(t *testing.T) {
	ctx := setupSyncModelFilterTest(t)
	if err := op.SettingSetString(model.SettingKeyModelFilterRegex, `(`); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}
	server := syncModelListServer(t, "gpt-4.1")
	channel := createAutoSyncChannelForModelFilterTest(t, ctx, "sync-invalid-filter-channel", server.URL, "preserve-me")

	report := SyncModelsTaskWithReport()
	if report.Error == "" {
		t.Fatalf("expected task-level error for invalid runtime filter, got %#v", report)
	}
	updated, err := op.ChannelGet(channel.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet failed: %v", err)
	}
	if updated.Model != "preserve-me" {
		t.Fatalf("invalid filter mutated models to %q", updated.Model)
	}
}
