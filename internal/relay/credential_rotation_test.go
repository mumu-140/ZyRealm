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

func TestHandlerCredentialFailureRotatesKeyWithinProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var firstHits atomic.Int32
	var secondHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer first-key":
			firstHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}`))
		case "Bearer second-key":
			secondHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_ok","object":"chat.completion","created":1,"model":"credential-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
		default:
			http.Error(w, "unexpected authorization", http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	channel := &dbmodel.Channel{
		Name:     "credential-rotation-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "credential-model",
		Keys: []dbmodel.ChannelKey{
			{Enabled: true, ChannelKey: "first-key", TotalCost: 0},
			{Enabled: true, ChannelKey: "second-key", TotalCost: 1},
		},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	group := &dbmodel.Group{Name: "credential-rotation-group", Mode: dbmodel.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: channel.ID, ModelName: "credential-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add group item: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"credential-rotation-group","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected request to recover through second key, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if firstHits.Load() != 1 || secondHits.Load() != 1 {
		t.Fatalf("expected one attempt per key, got first=%d second=%d", firstHits.Load(), secondHits.Load())
	}
	if availability.CredentialAvailable(channel.ID, channel.Keys[0].ID, time.Now()) {
		t.Fatalf("expected invalid first key to enter credential cooldown")
	}
	if !availability.CredentialAvailable(channel.ID, channel.Keys[1].ID, time.Now()) {
		t.Fatalf("expected successful second key to remain available")
	}
	if state := availability.CandidateState(channel.ID, "credential-model", time.Now()); state != availability.StateAvailable {
		t.Fatalf("expected credential failure not to degrade provider/model runtime state, got %v", state)
	}
}

func TestHandlerCredentialFailureCanRecoverOnThirdKeyWithoutSweepingPool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var firstHits atomic.Int32
	var secondHits atomic.Int32
	var thirdHits atomic.Int32
	var fourthHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer third-budget-key-1":
			firstHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}`))
		case "Bearer third-budget-key-2":
			secondHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}`))
		case "Bearer third-budget-key-3":
			thirdHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_ok","object":"chat.completion","created":1,"model":"credential-third-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
		case "Bearer third-budget-key-4":
			fourthHits.Add(1)
			http.Error(w, "fourth credential should not be used", http.StatusInternalServerError)
		default:
			http.Error(w, "unexpected authorization", http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	channel := &dbmodel.Channel{
		Name:     "credential-third-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "credential-third-model",
		Keys: []dbmodel.ChannelKey{
			{Enabled: true, ChannelKey: "third-budget-key-1"},
			{Enabled: true, ChannelKey: "third-budget-key-2"},
			{Enabled: true, ChannelKey: "third-budget-key-3"},
			{Enabled: true, ChannelKey: "third-budget-key-4"},
		},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	group := &dbmodel.Group{Name: "credential-third-group", Mode: dbmodel.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: channel.ID, ModelName: "credential-third-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add group item: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"credential-third-group","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected request to recover through third key, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if firstHits.Load() != 1 || secondHits.Load() != 1 || thirdHits.Load() != 1 {
		t.Fatalf("expected one attempt on each of first three keys, got first=%d second=%d third=%d", firstHits.Load(), secondHits.Load(), thirdHits.Load())
	}
	if fourthHits.Load() != 0 {
		t.Fatalf("expected fourth key to remain untouched after third-key success, got %d", fourthHits.Load())
	}
	if availability.CredentialAvailable(channel.ID, channel.Keys[0].ID, time.Now()) {
		t.Fatalf("expected first invalid key to enter credential cooldown")
	}
	if availability.CredentialAvailable(channel.ID, channel.Keys[1].ID, time.Now()) {
		t.Fatalf("expected second invalid key to enter credential cooldown")
	}
	if !availability.CredentialAvailable(channel.ID, channel.Keys[2].ID, time.Now()) {
		t.Fatalf("expected successful third key to remain available")
	}
	if !availability.CredentialAvailable(channel.ID, channel.Keys[3].ID, time.Now()) {
		t.Fatalf("expected untouched fourth key to remain available")
	}
}
