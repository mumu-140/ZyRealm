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

func TestHandlerModelCapacityUsesAlternativeProviderAndCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var firstHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		w.Header().Set("Retry-After", "1")
		http.Error(w, `{"error":{"message":"Upstream rate limit","type":"rate_limit"}}`, http.StatusTooManyRequests)
	}))
	defer first.Close()

	var secondHits atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_ok","object":"chat.completion","created":1,"model":"capacity-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer second.Close()

	firstChannel := &dbmodel.Channel{
		Name:     "model-capacity-first",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: first.URL + "/v1"}},
		Model:    "capacity-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "first-key"}},
	}
	if err := op.ChannelCreate(firstChannel, ctx); err != nil {
		t.Fatalf("create first channel: %v", err)
	}
	secondChannel := &dbmodel.Channel{
		Name:     "model-capacity-second",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: second.URL + "/v1"}},
		Model:    "capacity-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "second-key"}},
	}
	if err := op.ChannelCreate(secondChannel, ctx); err != nil {
		t.Fatalf("create second channel: %v", err)
	}

	group := &dbmodel.Group{
		Name:         "model-capacity-group",
		Mode:         dbmodel.GroupModeFailover,
		RetryEnabled: true,
		MaxRetries:   3,
	}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{GroupID: group.ID, ChannelID: firstChannel.ID, ModelName: "capacity-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("add first group item: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{GroupID: group.ID, ChannelID: secondChannel.ID, ModelName: "capacity-model", Priority: 2, Weight: 1}, ctx); err != nil {
		t.Fatalf("add second group item: %v", err)
	}

	makeRequest := func(content string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model-capacity-group","messages":[{"role":"user","content":"`+content+`"}]}`))
		c.Request.Header.Set("Content-Type", "application/json")
		Handler(inbound.InboundTypeOpenAIChat, c)
		return recorder
	}

	firstResponse := makeRequest("first")
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("expected first request to fail over successfully, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}
	if firstHits.Load() != 1 {
		t.Fatalf("expected model-capacity provider to be tried once without same-provider backoff, got %d", firstHits.Load())
	}
	if secondHits.Load() != 1 {
		t.Fatalf("expected alternative provider to be tried once, got %d", secondHits.Load())
	}
	if state := availability.CandidateState(firstChannel.ID, "capacity-model", time.Now()); state != availability.StateCooldown {
		t.Fatalf("expected first provider/model to enter cooldown, got %v", state)
	}

	secondResponse := makeRequest("second")
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("expected second request to use healthy provider, got %d: %s", secondResponse.Code, secondResponse.Body.String())
	}
	if firstHits.Load() != 1 {
		t.Fatalf("expected cooling provider/model to be filtered before routing, got %d total hits", firstHits.Load())
	}
	if secondHits.Load() != 2 {
		t.Fatalf("expected second provider to serve both requests, got %d hits", secondHits.Load())
	}
}

func TestHandlerSingleProviderModelCapacityEntersCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "1")
		http.Error(w, `{"error":{"message":"Upstream rate limit","type":"rate_limit"}}`, http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	channel := &dbmodel.Channel{
		Name:     "single-model-capacity",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL + "/v1"}},
		Model:    "capacity-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "single-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	group := &dbmodel.Group{Name: "single-capacity-group", Mode: dbmodel.GroupModeFailover, RetryEnabled: false}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "capacity-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("add group item: %v", err)
	}

	makeRequest := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"single-capacity-group","messages":[{"role":"user","content":"test"}]}`))
		c.Request.Header.Set("Content-Type", "application/json")
		Handler(inbound.InboundTypeOpenAIChat, c)
		return recorder
	}

	firstResponse := makeRequest()
	if firstResponse.Code != http.StatusTooManyRequests {
		t.Fatalf("expected first request to preserve upstream 429, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}
	if hits.Load() != 1 {
		t.Fatalf("expected one upstream attempt, got %d", hits.Load())
	}
	if state := availability.CandidateState(channel.ID, "capacity-model", time.Now()); state != availability.StateCooldown {
		t.Fatalf("expected single provider/model to enter cooldown, got %v", state)
	}

	secondResponse := makeRequest()
	if secondResponse.Code == http.StatusOK {
		t.Fatalf("expected second request to fail while only provider/model is cooling")
	}
	if hits.Load() != 1 {
		t.Fatalf("expected cooldown to block the immediate second upstream attempt, got %d total hits", hits.Load())
	}
}
