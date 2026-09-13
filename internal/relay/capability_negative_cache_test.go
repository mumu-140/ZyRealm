package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestHandlerCapabilityNegativeCacheSkipsOnlyMatchingShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var incompatibleHits atomic.Int32
	incompatible := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		incompatibleHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"reasoning effort xhigh is not supported","type":"invalid_request_error","code":"invalid_api_key"}}`))
	}))
	defer incompatible.Close()

	var healthyHits atomic.Int32
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_ok","object":"chat.completion","created":1,"model":"capability-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer healthy.Close()

	firstChannel := &dbmodel.Channel{
		Name:     "capability-cache-first",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: incompatible.URL + "/v1"}},
		Model:    "capability-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "first-key"}},
	}
	if err := op.ChannelCreate(firstChannel, ctx); err != nil {
		t.Fatalf("create first channel: %v", err)
	}
	secondChannel := &dbmodel.Channel{
		Name:     "capability-cache-second",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: healthy.URL + "/v1"}},
		Model:    "capability-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "second-key"}},
	}
	if err := op.ChannelCreate(secondChannel, ctx); err != nil {
		t.Fatalf("create second channel: %v", err)
	}

	group := &dbmodel.Group{Name: "capability-cache-group", Mode: dbmodel.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{GroupID: group.ID, ChannelID: firstChannel.ID, ModelName: "capability-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("add first group item: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{GroupID: group.ID, ChannelID: secondChannel.ID, ModelName: "capability-model", Priority: 2, Weight: 1}, ctx); err != nil {
		t.Fatalf("add second group item: %v", err)
	}

	makeRequest := func(content, effort string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		body := fmt.Sprintf(`{"model":"capability-cache-group","messages":[{"role":"user","content":%q}],"reasoning_effort":%q}`, content, effort)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		Handler(inbound.InboundTypeOpenAIChat, c)
		return recorder
	}

	first := makeRequest("first prompt", "xhigh")
	if first.Code != http.StatusOK {
		t.Fatalf("first request should fail over to healthy provider, got %d: %s", first.Code, first.Body.String())
	}
	if incompatibleHits.Load() != 1 || healthyHits.Load() != 1 {
		t.Fatalf("first request hits = incompatible:%d healthy:%d, want 1/1", incompatibleHits.Load(), healthyHits.Load())
	}

	second := makeRequest("different prompt", "xhigh")
	if second.Code != http.StatusOK {
		t.Fatalf("same capability shape should use healthy provider, got %d: %s", second.Code, second.Body.String())
	}
	if incompatibleHits.Load() != 1 || healthyHits.Load() != 2 {
		t.Fatalf("matching negative cache must skip incompatible provider before wire; hits=%d/%d", incompatibleHits.Load(), healthyHits.Load())
	}

	third := makeRequest("third prompt", "high")
	if third.Code != http.StatusOK {
		t.Fatalf("different capability shape should still fail over successfully, got %d: %s", third.Code, third.Body.String())
	}
	if incompatibleHits.Load() != 2 || healthyHits.Load() != 3 {
		t.Fatalf("different reasoning effort must not be overblocked; hits=%d/%d", incompatibleHits.Load(), healthyHits.Load())
	}
}
