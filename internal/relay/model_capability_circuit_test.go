package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestModelCapabilityMismatchDoesNotTripCircuit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"model low/medium/high/xhigh is not supported","type":"invalid_request_error","code":"invalid_api_key"}}`))
	}))
	defer server.Close()

	channel := &dbmodel.Channel{
		Name:     "capability-circuit-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "capability-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "healthy-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	group := &dbmodel.Group{Name: "capability-circuit-group", Mode: dbmodel.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: channel.ID, ModelName: "capability-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add group item: %v", err)
	}

	for i := 0; i < 5; i++ {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"capability-circuit-group","messages":[{"role":"user","content":"hello"}]}`))
		c.Request.Header.Set("Content-Type", "application/json")
		Handler(inbound.InboundTypeOpenAIChat, c)
		if i == 0 {
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("first request: expected upstream capability envelope to remain 401, got %d: %s", recorder.Code, recorder.Body.String())
			}
			continue
		}
		if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), capabilityNegativeCacheSkipPrefix) {
			t.Fatalf("request %d: expected cache-only 503 without another upstream attempt, got %d: %s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	if hits.Load() != 1 {
		t.Fatalf("expected repeated identical capability mismatch to hit upstream once, got %d", hits.Load())
	}
	if tripped, _ := balancer.IsTripped(channel.ID, channel.Keys[0].ID, "capability-model"); tripped {
		t.Fatalf("expected semantic model capability mismatch not to trip key/model circuit")
	}
}
