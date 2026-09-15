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
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestHandlerPassthroughUpstreamSSECommentDoesNotBlockFirstTokenFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var stalledHits atomic.Int32
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stalledHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(": keepalive\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer stalled.Close()

	var fallbackHits atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"fallback-ok\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer fallback.Close()

	stalledChannel := &dbmodel.Channel{
		Name:     "passthrough-keepalive-stalled",
		Type:     outbound.OutboundTypeAnthropic,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: stalled.URL + "/v1"}},
		Model:    "passthrough-keepalive-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "stalled-key"}},
	}
	if err := op.ChannelCreate(stalledChannel, ctx); err != nil {
		t.Fatalf("create stalled channel: %v", err)
	}

	fallbackChannel := &dbmodel.Channel{
		Name:     "passthrough-keepalive-fallback",
		Type:     outbound.OutboundTypeAnthropic,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: fallback.URL + "/v1"}},
		Model:    "passthrough-keepalive-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "fallback-key"}},
	}
	if err := op.ChannelCreate(fallbackChannel, ctx); err != nil {
		t.Fatalf("create fallback channel: %v", err)
	}

	group := &dbmodel.Group{
		Name:              "passthrough-keepalive-group",
		Mode:              dbmodel.GroupModeFailover,
		FirstTokenTimeOut: 1,
		RetryEnabled:      true,
		MaxRetries:        3,
	}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: stalledChannel.ID, ModelName: "passthrough-keepalive-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add stalled group item: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: fallbackChannel.ID, ModelName: "passthrough-keepalive-model", Priority: 2, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add fallback group item: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"passthrough-keepalive-group","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Anthropic-Version", "2023-06-01")
	Handler(inbound.InboundTypeAnthropic, c)

	if stalledHits.Load() != 1 {
		t.Fatalf("expected stalled passthrough provider once, got %d", stalledHits.Load())
	}
	if fallbackHits.Load() != 1 {
		t.Fatalf("expected fallback provider after comment-only first-token timeout, got %d", fallbackHits.Load())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, ": keepalive\n\n") {
		t.Fatalf("expected upstream comment to remain byte-preserved downstream, got %q", body)
	}
	if !strings.Contains(body, "fallback-ok") {
		t.Fatalf("expected fallback provider payload after upstream comment, got %q", body)
	}
}
