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

func TestHandlerDoesNotReplayAfterCommittedStreamOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var interruptedHits atomic.Int32
	interrupted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		interruptedHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_partial\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"stream-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// Abort the HTTP response after a real SSE payload has already been
		// delivered. The relay may record the upstream stream failure, but it
		// must not replay the request to another provider after client output.
		panic(http.ErrAbortHandler)
	}))
	defer interrupted.Close()

	var fallbackHits atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_fallback\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"stream-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"must-not-replay\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"))
	}))
	defer fallback.Close()

	interruptedChannel := &dbmodel.Channel{
		Name:     "committed-stream-first",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: interrupted.URL + "/v1"}},
		Model:    "stream-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "first-key"}},
	}
	if err := op.ChannelCreate(interruptedChannel, ctx); err != nil {
		t.Fatalf("create interrupted channel: %v", err)
	}
	fallbackChannel := &dbmodel.Channel{
		Name:     "committed-stream-second",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: fallback.URL + "/v1"}},
		Model:    "stream-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "second-key"}},
	}
	if err := op.ChannelCreate(fallbackChannel, ctx); err != nil {
		t.Fatalf("create fallback channel: %v", err)
	}

	group := &dbmodel.Group{
		Name:         "committed-stream-group",
		Mode:         dbmodel.GroupModeFailover,
		RetryEnabled: true,
		MaxRetries:   3,
	}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: interruptedChannel.ID, ModelName: "stream-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add interrupted group item: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: fallbackChannel.ID, ModelName: "stream-model", Priority: 2, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add fallback group item: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"committed-stream-group","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)

	if interruptedHits.Load() != 1 {
		t.Fatalf("expected interrupted provider to be attempted once, got %d", interruptedHits.Load())
	}
	if fallbackHits.Load() != 0 {
		t.Fatalf("expected no cross-provider replay after committed output, fallback hits = %d", fallbackHits.Load())
	}
	if !strings.Contains(recorder.Body.String(), "partial") {
		t.Fatalf("expected already-committed upstream payload to remain visible, got %q", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "must-not-replay") {
		t.Fatalf("fallback payload must never be appended after committed output: %q", recorder.Body.String())
	}
}
