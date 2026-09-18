package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestContentPolicyFailureRemainsTerminalAndRuntimeNeutral(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"sensitive_words_detected","type":"content_policy_violation"}}`))
	}))
	defer server.Close()

	channel := &dbmodel.Channel{
		Name:     "content-policy-runtime-neutral-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "policy-runtime-neutral-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "policy-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	group := &dbmodel.Group{Name: "content-policy-runtime-neutral-group", Mode: dbmodel.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: channel.ID, ModelName: "policy-runtime-neutral-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add group item: %v", err)
	}

	for i := 0; i < 5; i++ {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"content-policy-runtime-neutral-group","messages":[{"role":"user","content":"blocked prompt"}]}`))
		c.Request.Header.Set("Content-Type", "application/json")
		Handler(inbound.InboundTypeOpenAIChat, c)
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("request %d: expected terminal content policy 500, got %d: %s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	if state := availability.CandidateState(channel.ID, "policy-runtime-neutral-model", time.Now()); state != availability.StateAvailable {
		t.Fatalf("content-policy runtime state = %v, want available", state)
	}
}
