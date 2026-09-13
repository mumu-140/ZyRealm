package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestContentPolicyFailureDoesNotTripCircuit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"sensitive_words_detected","type":"content_policy_violation"}}`))
	}))
	defer server.Close()

	channel := &dbmodel.Channel{
		Name:     "content-policy-circuit-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "policy-circuit-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "policy-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	group := &dbmodel.Group{Name: "content-policy-circuit-group", Mode: dbmodel.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: channel.ID, ModelName: "policy-circuit-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add group item: %v", err)
	}

	for i := 0; i < 5; i++ {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"content-policy-circuit-group","messages":[{"role":"user","content":"blocked prompt"}]}`))
		c.Request.Header.Set("Content-Type", "application/json")
		Handler(inbound.InboundTypeOpenAIChat, c)
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("request %d: expected terminal content policy 500, got %d: %s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	if tripped, _ := balancer.IsTripped(channel.ID, channel.Keys[0].ID, "policy-circuit-model"); tripped {
		t.Fatalf("expected explicit content policy failures not to trip key/model circuit")
	}
}
