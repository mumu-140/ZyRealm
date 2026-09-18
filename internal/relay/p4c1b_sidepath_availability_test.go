package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

type p4c1bRoute struct {
	channel       *model.Channel
	group         *model.Group
	upstreamModel string
}

func newP4C1BCompactRoute(t *testing.T, ctx context.Context, upstreamURL, suffix string, keys []model.ChannelKey) p4c1bRoute {
	t.Helper()
	upstreamModel := "p4c1b-compact-upstream-" + suffix
	channel := &model.Channel{
		Name:      "p4c1b-compact-channel-" + suffix,
		Type:      outbound.OutboundTypeOpenAIResponse,
		Enabled:   true,
		BaseUrls:  []model.BaseUrl{{URL: upstreamURL + "/v1"}},
		Model:     upstreamModel,
		ProxyMode: model.ProxyUsageModeDirect,
		Keys:      keys,
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	group := &model.Group{Name: "p4c1b-compact-group-" + suffix, Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{
		GroupID: group.ID, ChannelID: channel.ID, ModelName: upstreamModel, Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}
	return p4c1bRoute{channel: channel, group: group, upstreamModel: upstreamModel}
}

func runP4C1BCompact(t *testing.T, groupName string, apiKeyID int) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", apiKeyID)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(
		`{"model":"`+groupName+`","input":"hello"}`,
	))
	c.Request.Header.Set("Content-Type", "application/json")
	HandleResponsesCompact(c)
	return recorder
}

func writeP4C1BCompactSuccess(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"id":"cmp_1","object":"response.compaction","created_at":1,"output":[]}`)
}

func writeP4C1BProviderUnavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, `{"error":{"message":"upstream service temporarily unavailable"}}`)
}

func newP4C1BWSRoute(t *testing.T, ctx context.Context, upstreamURL, suffix string, keys []model.ChannelKey) p4c1bRoute {
	t.Helper()
	upstreamModel := "p4c1b-ws-upstream-" + suffix
	channel := &model.Channel{
		Name:      "p4c1b-ws-channel-" + suffix,
		Type:      outbound.OutboundTypeOpenAIChat,
		Enabled:   true,
		BaseUrls:  []model.BaseUrl{{URL: upstreamURL + "/v1"}},
		Model:     upstreamModel,
		ProxyMode: model.ProxyUsageModeDirect,
		Keys:      keys,
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	group := &model.Group{Name: "p4c1b-ws-group-" + suffix, Mode: model.GroupModeFailover, SessionKeepTime: 60}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{
		GroupID: group.ID, ChannelID: channel.ID, ModelName: upstreamModel, Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}
	return p4c1bRoute{channel: channel, group: group, upstreamModel: upstreamModel}
}

// These P4C1b WebSocket routing tests deliberately use a non-2xx upstream
// response. A real processWSResponseCreate round always forces stream=true; a
// direct runWSRelay success fixture without that wrapper would exercise the
// ordinary Gin non-stream renderer with req.c == nil, which is not the live WS
func newP4C1BWSRelayRequest(t *testing.T, ctx context.Context, route p4c1bRoute, apiKeyID int) (*relayRequest, *model.Group) {
	t.Helper()
	clientConn, serverConn := newTestWSConnPair(t)
	t.Cleanup(func() {
		clientConn.Close(websocket.StatusNormalClosure, "")
		serverConn.Close(websocket.StatusNormalClosure, "")
	})

	rawBody := []byte(`{"model":"` + route.group.Name + `","messages":[{"role":"user","content":"hi"}]}`)
	newInternal := func() *transformerModel.InternalLLMRequest {
		return &transformerModel.InternalLLMRequest{
			Model:        route.group.Name,
			RawAPIFormat: transformerModel.APIFormatOpenAIChatCompletion,
			Messages: []transformerModel.Message{{
				Role: "user", Content: transformerModel.MessageContent{Content: stringPtr("hi")},
			}},
		}
	}
	req, group, err := newWSRelayRequest(
		ctx,
		serverConn,
		inbound.Get(inbound.InboundTypeOpenAIChat),
		apiKeyID,
		route.group.Name,
		newInternal(),
		newInternal(),
		nil,
		rawBody,
	)
	if err != nil {
		t.Fatalf("newWSRelayRequest failed: %v", err)
	}
	return req, group
}

func newP4C1BWarmupRoute(t *testing.T, ctx context.Context, upstreamURL, suffix string) p4c1bRoute {
	t.Helper()
	upstreamModel := "p4c1b-warmup-upstream-" + suffix
	channel := &model.Channel{
		Name:     "p4c1b-warmup-channel-" + suffix,
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: upstreamURL + "/v1"}},
		Model:    upstreamModel,
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "warmup-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	group := &model.Group{Name: "p4c1b-warmup-group-" + suffix, Mode: model.GroupModeFailover, SessionKeepTime: 60}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: upstreamModel, Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}
	return p4c1bRoute{channel: channel, group: group, upstreamModel: upstreamModel}
}

func newP4C1BWarmupServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		hits.Add(1)
		defer conn.CloseNow()
		<-release
	}))
	t.Cleanup(func() {
		once.Do(func() { close(release) })
		server.Close()
		resetWSUpstreamPool()
	})
	return server, &hits
}

func p4c1bWarmupBody(groupName string) map[string]json.RawMessage {
	encodedModel, _ := json.Marshal(groupName)
	return map[string]json.RawMessage{
		"model":    encodedModel,
		"generate": json.RawMessage(`false`),
	}
}

func TestP4C1BWarmupSkipsRuntimeCooldownWithoutWritingHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)
	resetWSUpstreamPool()
	server, hits := newP4C1BWarmupServer(t)
	route := newP4C1BWarmupRoute(t, ctx, server.URL, "cooldown")

	availability.RecordProviderFailure(route.channel.ID, "seed", time.Now())
	before := availability.CandidateInfo(route.channel.ID, route.upstreamModel, time.Now())
	if before.State != availability.StateCooldown {
		t.Fatalf("test precondition: state = %v, want cooldown", before.State)
	}

	if err := bestEffortWarmupUpstreamWS(ctx, 4301, "", p4c1bWarmupBody(route.group.Name)); err == nil {
		t.Fatal("warmup unexpectedly used a runtime-cooling candidate")
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("warmup upstream hits = %d, want 0 during runtime cooldown", got)
	}
	after := availability.CandidateInfo(route.channel.ID, route.upstreamModel, time.Now())
	if after.State != availability.StateCooldown || after.Reason != before.Reason || !after.CooldownUntil.Equal(before.CooldownUntil) {
		t.Fatalf("warmup mutated runtime health: before=%+v after=%+v", before, after)
	}
}

func TestP4C1BWarmupDoesNotStealHalfOpenProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)
	resetWSUpstreamPool()
	server, hits := newP4C1BWarmupServer(t)
	route := newP4C1BWarmupRoute(t, ctx, server.URL, "half-open")

	availability.RecordProviderFailure(route.channel.ID, "seed", time.Now().Add(-10*time.Second))
	if state := availability.CandidateState(route.channel.ID, route.upstreamModel, time.Now()); state != availability.StateHalfOpen {
		t.Fatalf("test precondition: state = %v, want half-open", state)
	}
	if err := bestEffortWarmupUpstreamWS(ctx, 4302, "", p4c1bWarmupBody(route.group.Name)); err == nil {
		t.Fatal("warmup unexpectedly consumed a half-open candidate")
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("warmup upstream hits = %d, want 0 while candidate awaits a real half-open probe", got)
	}

	lease, ok := availability.AcquireCandidate(route.channel.ID, route.upstreamModel, time.Now())
	if !ok {
		t.Fatal("real request could not acquire half-open probe after warmup; warmup stole the recovery lease")
	}
	availability.ReleaseLease(lease, time.Now())
}
