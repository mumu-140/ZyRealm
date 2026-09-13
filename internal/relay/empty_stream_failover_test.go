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

func TestHandlerEmptyStreamImmediatelyFailsOverAndCoolsModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var emptyHits atomic.Int32
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		emptyHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer empty.Close()

	var healthyHits atomic.Int32
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_ok\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"stream-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"))
	}))
	defer healthy.Close()

	emptyChannel := &dbmodel.Channel{
		Name:     "empty-stream-first",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: empty.URL + "/v1"}},
		Model:    "stream-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "empty-key"}},
	}
	if err := op.ChannelCreate(emptyChannel, ctx); err != nil {
		t.Fatalf("create empty channel: %v", err)
	}
	healthyChannel := &dbmodel.Channel{
		Name:     "empty-stream-second",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: healthy.URL + "/v1"}},
		Model:    "stream-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "healthy-key"}},
	}
	if err := op.ChannelCreate(healthyChannel, ctx); err != nil {
		t.Fatalf("create healthy channel: %v", err)
	}

	group := &dbmodel.Group{
		Name:         "empty-stream-group",
		Mode:         dbmodel.GroupModeFailover,
		RetryEnabled: true,
		MaxRetries:   3,
	}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: emptyChannel.ID, ModelName: "stream-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add empty group item: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: healthyChannel.ID, ModelName: "stream-model", Priority: 2, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add healthy group item: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"empty-stream-group","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected empty stream to fail over successfully, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if emptyHits.Load() != 1 {
		t.Fatalf("expected empty-stream provider to be tried once, got %d", emptyHits.Load())
	}
	if healthyHits.Load() != 1 {
		t.Fatalf("expected healthy provider to be tried once, got %d", healthyHits.Load())
	}
	if !strings.Contains(recorder.Body.String(), "ok") {
		t.Fatalf("expected healthy provider payload, got %q", recorder.Body.String())
	}
	if state := availability.CandidateState(emptyChannel.ID, "stream-model", time.Now()); state != availability.StateCooldown {
		t.Fatalf("expected empty-stream provider/model to enter cooldown, got %v", state)
	}
}
