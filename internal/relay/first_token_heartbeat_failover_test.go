package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestHandlerFirstTokenTimeoutFailsOverAfterEarlyHeartbeat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	setHeartbeatSettings(t, "1", "1")

	var stalledHits atomic.Int32
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stalledHits.Add(1)
		<-r.Context().Done()
	}))
	defer stalled.Close()

	var fallbackHits atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_fallback\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"heartbeat-timeout-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"fallback-ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"))
	}))
	defer fallback.Close()

	stalledChannel := &dbmodel.Channel{
		Name:     "heartbeat-timeout-stalled",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: stalled.URL + "/v1"}},
		Model:    "heartbeat-timeout-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "stalled-key"}},
	}
	if err := op.ChannelCreate(stalledChannel, ctx); err != nil {
		t.Fatalf("create stalled channel: %v", err)
	}
	fallbackChannel := &dbmodel.Channel{
		Name:     "heartbeat-timeout-fallback",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: fallback.URL + "/v1"}},
		Model:    "heartbeat-timeout-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "fallback-key"}},
	}
	if err := op.ChannelCreate(fallbackChannel, ctx); err != nil {
		t.Fatalf("create fallback channel: %v", err)
	}

	group := &dbmodel.Group{
		Name:              "heartbeat-timeout-group",
		Mode:              dbmodel.GroupModeFailover,
		FirstTokenTimeOut: 2,
		RetryEnabled:      true,
		MaxRetries:        3,
	}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: stalledChannel.ID, ModelName: "heartbeat-timeout-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add stalled group item: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: fallbackChannel.ID, ModelName: "heartbeat-timeout-model", Priority: 2, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add fallback group item: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"heartbeat-timeout-group","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)

	if stalledHits.Load() != 1 {
		t.Fatalf("expected stalled provider to be attempted once, got %d", stalledHits.Load())
	}
	if fallbackHits.Load() != 1 {
		t.Fatalf("expected fallback provider after heartbeat-only first-token timeout, got %d hits", fallbackHits.Load())
	}
	if !strings.Contains(recorder.Body.String(), "fallback-ok") {
		t.Fatalf("expected fallback model payload after heartbeat, got %q", recorder.Body.String())
	}
}
