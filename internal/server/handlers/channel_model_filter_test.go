package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func setupChannelModelFilterHandlerTest(t *testing.T) {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	if err := dbpkg.InitDB("sqlite", filepath.Join(t.TempDir(), "handler-model-filter.db"), false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("InitCache failed: %v", err)
	}
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { _ = dbpkg.Close() })
}

func setGlobalModelFilterForHandlerTest(t *testing.T, pattern string) {
	t.Helper()
	if err := op.SettingSetString(model.SettingKeyModelFilterRegex, pattern); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}
}

func newModelListServer(t *testing.T, models ...string) *httptest.Server {
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

func performJSONHandler(t *testing.T, handler gin.HandlerFunc, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler(ctx)
	return recorder
}

func decodeResponseDataStrings(t *testing.T, recorder *httptest.ResponseRecorder) []string {
	t.Helper()
	var payload struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response failed: %v; body=%s", err, recorder.Body.String())
	}
	return payload.Data
}

func TestFetchModelAppliesGlobalAndChannelFilters(t *testing.T) {
	setupChannelModelFilterHandlerTest(t)
	setGlobalModelFilterForHandlerTest(t, `^gpt-`)
	server := newModelListServer(t, "claude-mini", "gpt-4o", "gpt-4o-mini", "gpt-4.1-mini")
	channelPattern := `mini$`

	recorder := performJSONHandler(t, fetchModel, model.Channel{
		Type:       outbound.OutboundTypeOpenAIChat,
		BaseUrls:   []model.BaseUrl{{URL: server.URL}},
		Keys:       []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
		MatchRegex: &channelPattern,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	got := decodeResponseDataStrings(t, recorder)
	want := []string{"gpt-4o-mini", "gpt-4.1-mini"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("models=%#v, want %#v", got, want)
	}
}

func TestBatchRefreshPersistsOnlyGloballyAdmittedModels(t *testing.T) {
	setupChannelModelFilterHandlerTest(t)
	setGlobalModelFilterForHandlerTest(t, `^gpt-`)
	server := newModelListServer(t, "claude-3-5-sonnet", "gpt-4o", "gpt-4.1")
	ctx := context.Background()
	channel := &model.Channel{
		Name:     "batch-filter-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
		Model:    "old-model",
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	recorder := performJSONHandler(t, batchUpdateChannels, model.ChannelBatchUpdateRequest{
		IDs:           []int{channel.ID},
		RefreshModels: true,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	updated, err := op.ChannelGet(channel.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet failed: %v", err)
	}
	if updated.Model != "gpt-4o,gpt-4.1" {
		t.Fatalf("persisted models=%q, want %q", updated.Model, "gpt-4o,gpt-4.1")
	}
}

func TestBatchRefreshInvalidGlobalFilterDoesNotMutateChannel(t *testing.T) {
	setupChannelModelFilterHandlerTest(t)
	setGlobalModelFilterForHandlerTest(t, `(`)
	server := newModelListServer(t, "gpt-4o")
	ctx := context.Background()
	channel := &model.Channel{
		Name:           "batch-invalid-filter-channel",
		Type:           outbound.OutboundTypeOpenAIChat,
		Enabled:        true,
		MaxConcurrency: 3,
		BaseUrls:       []model.BaseUrl{{URL: server.URL}},
		Keys:           []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
		Model:          "preserve-me",
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	newLimit := 9

	recorder := performJSONHandler(t, batchUpdateChannels, model.ChannelBatchUpdateRequest{
		IDs:            []int{channel.ID},
		MaxConcurrency: &newLimit,
		RefreshModels:   true,
	})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s, want 500 for invalid runtime filter", recorder.Code, recorder.Body.String())
	}
	updated, err := op.ChannelGet(channel.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet failed: %v", err)
	}
	if updated.Model != "preserve-me" || updated.MaxConcurrency != 3 {
		t.Fatalf("channel mutated under invalid global regex: model=%q max_concurrency=%d", updated.Model, updated.MaxConcurrency)
	}
}
