package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestHandlerGeneric500SearchesAlternativeProviderBeforeSameKeyRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var firstHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"opaque upstream failure"}}`))
	}))
	defer first.Close()

	var secondHits atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","object":"chat.completion","created":1,"model":"search-first-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer second.Close()

	firstChannel := &dbmodel.Channel{
		Name: "search-first-500", Type: outbound.OutboundTypeOpenAIChat, Enabled: true,
		BaseUrls: []dbmodel.BaseUrl{{URL: first.URL + "/v1"}}, Model: "search-first-model",
		Keys: []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "test-a"}},
	}
	if err := op.ChannelCreate(firstChannel, ctx); err != nil {
		t.Fatalf("create first channel: %v", err)
	}
	secondChannel := &dbmodel.Channel{
		Name: "search-first-backup", Type: outbound.OutboundTypeOpenAIChat, Enabled: true,
		BaseUrls: []dbmodel.BaseUrl{{URL: second.URL + "/v1"}}, Model: "search-first-model",
		Keys: []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "test-b"}},
	}
	if err := op.ChannelCreate(secondChannel, ctx); err != nil {
		t.Fatalf("create second channel: %v", err)
	}

	group := &dbmodel.Group{
		Name: "search-first-group", Mode: dbmodel.GroupModeFailover,
		RetryEnabled: true, MaxRetries: 3,
	}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	for _, item := range []dbmodel.GroupItem{
		{GroupID: group.ID, ChannelID: firstChannel.ID, ModelName: "search-first-model", Priority: 1, Weight: 1},
		{GroupID: group.ID, ChannelID: secondChannel.ID, ModelName: "search-first-model", Priority: 2, Weight: 1},
	} {
		item := item
		if err := op.GroupItemAdd(&item, ctx); err != nil {
			t.Fatalf("add group item: %v", err)
		}
	}

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"search-first-group","messages":[{"role":"user","content":"hello"}]}`))
	ginCtx.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, ginCtx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected backup provider to recover request, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if firstHits.Load() != 1 {
		t.Fatalf("generic 500 should switch provider before same-key retry, first hits=%d", firstHits.Load())
	}
	if secondHits.Load() != 1 {
		t.Fatalf("expected one backup provider hit, got %d", secondHits.Load())
	}
	if state := availability.CandidateState(firstChannel.ID, "search-first-model", time.Now()); state != availability.StateAvailable {
		t.Fatalf("generic 500 search-first routing must remain runtime-neutral, got %v", state)
	}
}

func TestHandlerLastProviderCanExploreBeyondThreeCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n <= 3 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key","code":"invalid_api_key"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"ok","object":"chat.completion","created":1,"model":"last-provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer upstream.Close()

	channel := &dbmodel.Channel{
		Name: "last-provider-many-keys", Type: outbound.OutboundTypeOpenAIChat, Enabled: true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL + "/v1"}}, Model: "last-provider-model",
		Keys: []dbmodel.ChannelKey{
			{Enabled: true, ChannelKey: "test-1"},
			{Enabled: true, ChannelKey: "test-2"},
			{Enabled: true, ChannelKey: "test-3"},
			{Enabled: true, ChannelKey: "test-4"},
		},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	group := &dbmodel.Group{Name: "last-provider-group", Mode: dbmodel.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: channel.ID, ModelName: "last-provider-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add group item: %v", err)
	}

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"last-provider-group","messages":[{"role":"user","content":"hello"}]}`))
	ginCtx.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, ginCtx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected fourth credential to recover request, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if hits.Load() != 4 {
		t.Fatalf("expected four wire attempts to find a usable credential, got %d", hits.Load())
	}
}
