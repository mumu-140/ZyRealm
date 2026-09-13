package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestHandlerCredentialPoolUsesProviderLocalFairLedger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		mu.Lock()
		hits[auth]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_ok","object":"chat.completion","created":1,"model":"fair-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	channel := &dbmodel.Channel{
		Name:     "credential-fair-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "fair-model",
		Keys: []dbmodel.ChannelKey{
			{Enabled: true, ChannelKey: "fair-key-1", TotalCost: 0},
			{Enabled: true, ChannelKey: "fair-key-2", TotalCost: 1000},
			{Enabled: true, ChannelKey: "fair-key-3", TotalCost: 1000000},
		},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	group := &dbmodel.Group{Name: "credential-fair-group", Mode: dbmodel.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: channel.ID, ModelName: "fair-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add group item: %v", err)
	}

	for i := 0; i < 6; i++ {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"credential-fair-group","messages":[{"role":"user","content":"hello"}]}`))
		c.Request.Header.Set("Content-Type", "application/json")
		Handler(inbound.InboundTypeOpenAIChat, c)
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d returned %d: %s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for _, key := range []string{"fair-key-1", "fair-key-2", "fair-key-3"} {
		if got := hits["Bearer "+key]; got != 2 {
			t.Fatalf("hits=%v; %s got %d, want 2", hits, key, got)
		}
	}
}
