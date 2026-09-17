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

func TestHandlerCredentialBudgetFallsThroughToNextProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var firstKeyHits atomic.Int32
	var secondKeyHits atomic.Int32
	var thirdKeyHits atomic.Int32
	var fourthKeyHits atomic.Int32
	firstProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer bad-key-1":
			firstKeyHits.Add(1)
		case "Bearer bad-key-2":
			secondKeyHits.Add(1)
		case "Bearer bad-key-3":
			thirdKeyHits.Add(1)
		case "Bearer bad-key-4":
			fourthKeyHits.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}`))
	}))
	defer firstProvider.Close()

	var secondProviderHits atomic.Int32
	secondProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondProviderHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_ok","object":"chat.completion","created":1,"model":"credential-budget-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer secondProvider.Close()

	firstChannel := &dbmodel.Channel{
		Name:     "credential-budget-first",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: firstProvider.URL + "/v1"}},
		Model:    "credential-budget-model",
		Keys: []dbmodel.ChannelKey{
			{Enabled: true, ChannelKey: "bad-key-1", TotalCost: 0},
			{Enabled: true, ChannelKey: "bad-key-2", TotalCost: 1},
			{Enabled: true, ChannelKey: "bad-key-3", TotalCost: 2},
			{Enabled: true, ChannelKey: "bad-key-4", TotalCost: 3},
		},
	}
	if err := op.ChannelCreate(firstChannel, ctx); err != nil {
		t.Fatalf("create first channel: %v", err)
	}
	secondChannel := &dbmodel.Channel{
		Name:     "credential-budget-second",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: secondProvider.URL + "/v1"}},
		Model:    "credential-budget-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "good-key"}},
	}
	if err := op.ChannelCreate(secondChannel, ctx); err != nil {
		t.Fatalf("create second channel: %v", err)
	}

	group := &dbmodel.Group{Name: "credential-budget-group", Mode: dbmodel.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: firstChannel.ID, ModelName: "credential-budget-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add first group item: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: secondChannel.ID, ModelName: "credential-budget-model", Priority: 2, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add second group item: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"credential-budget-group","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected request to recover through second provider, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if firstKeyHits.Load() != 1 || secondKeyHits.Load() != 1 || thirdKeyHits.Load() != 1 {
		t.Fatalf("expected exactly three credential attempts on first provider, got key1=%d key2=%d key3=%d", firstKeyHits.Load(), secondKeyHits.Load(), thirdKeyHits.Load())
	}
	if fourthKeyHits.Load() != 0 {
		t.Fatalf("expected fourth key to remain untouched after credential budget, got %d hits", fourthKeyHits.Load())
	}
	if secondProviderHits.Load() != 1 {
		t.Fatalf("expected one attempt on fallback provider, got %d", secondProviderHits.Load())
	}

	for i := 0; i < 3; i++ {
		if availability.CredentialAvailable(firstChannel.ID, firstChannel.Keys[i].ID, time.Now()) {
			t.Fatalf("expected attempted key %d to enter cooldown", i+1)
		}
	}
	if !availability.CredentialAvailable(firstChannel.ID, firstChannel.Keys[3].ID, time.Now()) {
		t.Fatalf("expected untouched fourth key to remain available")
	}
	if state := availability.CandidateState(firstChannel.ID, "credential-budget-model", time.Now()); state != availability.StateAvailable {
		t.Fatalf("expected credential failures not to degrade first provider/model runtime state, got %v", state)
	}
}
