package relay

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestNearestRuntimeRecoveryRequiresAllCandidatesCooling(t *testing.T) {
	availability.Reset()
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	items := []model.GroupItem{
		{ChannelID: 101, ModelName: "model-a"},
		{ChannelID: 102, ModelName: "model-a"},
	}

	availability.RecordModelFailureWithRetryAfter(101, "model-a", "model_capacity", base, 8*time.Second)
	if _, ok := nearestRuntimeRecovery(items, "group-model", base); ok {
		t.Fatal("expected no all-cooling recovery while one candidate remains available")
	}

	availability.RecordModelFailureWithRetryAfter(102, "model-a", "model_capacity", base, 3*time.Second)
	until, ok := nearestRuntimeRecovery(items, "group-model", base)
	if !ok {
		t.Fatal("expected all-cooling recovery deadline")
	}
	if want := base.Add(3 * time.Second); !until.Equal(want) {
		t.Fatalf("nearest recovery = %v, want %v", until, want)
	}
}

func TestHandlerAllRuntimeCandidatesCoolingReturnsNearestRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var firstHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		w.Header().Set("Retry-After", "2")
		http.Error(w, `{"error":{"message":"rate limit exceeded"}}`, http.StatusTooManyRequests)
	}))
	defer first.Close()

	var secondHits atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		w.Header().Set("Retry-After", "5")
		http.Error(w, `{"error":{"message":"rate limit exceeded"}}`, http.StatusTooManyRequests)
	}))
	defer second.Close()

	firstChannel := &model.Channel{
		Name:     "runtime-recovery-first",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: first.URL + "/v1"}},
		Model:    "recovery-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "first-key"}},
	}
	if err := op.ChannelCreate(firstChannel, ctx); err != nil {
		t.Fatalf("create first channel: %v", err)
	}
	secondChannel := &model.Channel{
		Name:     "runtime-recovery-second",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: second.URL + "/v1"}},
		Model:    "recovery-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "second-key"}},
	}
	if err := op.ChannelCreate(secondChannel, ctx); err != nil {
		t.Fatalf("create second channel: %v", err)
	}

	group := &model.Group{Name: "runtime-recovery-group", Mode: model.GroupModeFailover, RetryEnabled: false}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: firstChannel.ID, ModelName: "recovery-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("add first item: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: secondChannel.ID, ModelName: "recovery-model", Priority: 2, Weight: 1}, ctx); err != nil {
		t.Fatalf("add second item: %v", err)
	}

	makeRequest := func(content string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"runtime-recovery-group","messages":[{"role":"user","content":"`+content+`"}]}`))
		c.Request.Header.Set("Content-Type", "application/json")
		Handler(inbound.InboundTypeOpenAIChat, c)
		return recorder
	}

	firstResponse := makeRequest("first")
	if firstResponse.Code != http.StatusTooManyRequests {
		t.Fatalf("expected exhausted capacity request to preserve 429, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}
	if firstHits.Load() != 1 || secondHits.Load() != 1 {
		t.Fatalf("expected both providers once, hits=(%d,%d)", firstHits.Load(), secondHits.Load())
	}

	secondResponse := makeRequest("second")
	if secondResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected all-cooling request to fail fast with 503, got %d: %s", secondResponse.Code, secondResponse.Body.String())
	}
	seconds, err := strconv.Atoi(secondResponse.Header().Get("Retry-After"))
	if err != nil || seconds < 1 || seconds > 2 {
		t.Fatalf("expected nearest Retry-After in [1,2], got %q", secondResponse.Header().Get("Retry-After"))
	}
	if firstHits.Load() != 1 || secondHits.Load() != 1 {
		t.Fatalf("expected no upstream calls while all candidates cool, hits=(%d,%d)", firstHits.Load(), secondHits.Load())
	}
	if !strings.Contains(secondResponse.Body.String(), "cooling down") {
		t.Fatalf("expected explicit cooling-down error, got %s", secondResponse.Body.String())
	}
}
