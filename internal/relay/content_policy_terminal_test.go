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

func TestHandlerContentPolicyDoesNotFailOverToAnotherProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var blockedHits atomic.Int32
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blockedHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"sensitive_words_detected","type":"content_policy_violation"}}`))
	}))
	defer blocked.Close()

	var fallbackHits atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_ok","object":"chat.completion","created":1,"model":"policy-model","choices":[{"index":0,"message":{"role":"assistant","content":"should-not-be-used"}}]}`))
	}))
	defer fallback.Close()

	blockedChannel := &dbmodel.Channel{
		Name:     "content-policy-blocked",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: blocked.URL + "/v1"}},
		Model:    "policy-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "blocked-key"}},
	}
	if err := op.ChannelCreate(blockedChannel, ctx); err != nil {
		t.Fatalf("create blocked channel: %v", err)
	}
	fallbackChannel := &dbmodel.Channel{
		Name:     "content-policy-fallback",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: fallback.URL + "/v1"}},
		Model:    "policy-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "fallback-key"}},
	}
	if err := op.ChannelCreate(fallbackChannel, ctx); err != nil {
		t.Fatalf("create fallback channel: %v", err)
	}

	group := &dbmodel.Group{Name: "content-policy-group", Mode: dbmodel.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: blockedChannel.ID, ModelName: "policy-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add blocked group item: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: fallbackChannel.ID, ModelName: "policy-model", Priority: 2, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add fallback group item: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"content-policy-group","messages":[{"role":"user","content":"blocked prompt"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected explicit content policy failure to remain terminal 500, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if blockedHits.Load() != 1 {
		t.Fatalf("expected blocked provider to be attempted once, got %d", blockedHits.Load())
	}
	if fallbackHits.Load() != 0 {
		t.Fatalf("expected no cross-provider failover for explicit content policy, got %d fallback hits", fallbackHits.Load())
	}
}
