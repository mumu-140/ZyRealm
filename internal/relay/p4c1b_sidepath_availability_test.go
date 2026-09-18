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
	"github.com/bestruirui/octopus/internal/relay/balancer"
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
// path. A 503 still proves credential/candidate admission and produces the same
// RoutingDecision health evidence without testing an artificial transport mode.
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

func TestP4C1BCompactSkipsCredentialInAvailabilityCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)

	var gotAuth atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		writeP4C1BCompactSuccess(w)
	}))
	defer server.Close()

	route := newP4C1BCompactRoute(t, ctx, server.URL, "credential-cooldown", []model.ChannelKey{
		{Enabled: true, ChannelKey: "compact-key-one", TotalCost: 0},
		{Enabled: true, ChannelKey: "compact-key-two", TotalCost: 1},
	})
	first := route.channel.Keys[0]
	availability.RecordCredentialFailureRevision(route.channel.ID, first.ID, first.CredentialRevision, "test", time.Now())

	recorder := runP4C1BCompact(t, route.group.Name, 4101)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if auth, _ := gotAuth.Load().(string); auth != "Bearer compact-key-two" {
		t.Fatalf("Authorization = %q, want second available credential", auth)
	}
}

func TestP4C1BCompactProviderFailureCreatesRuntimeCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writeP4C1BProviderUnavailable(w)
	}))
	defer server.Close()

	route := newP4C1BCompactRoute(t, ctx, server.URL, "provider-cooldown", []model.ChannelKey{{Enabled: true, ChannelKey: "compact-key"}})
	first := runP4C1BCompact(t, route.group.Name, 4102)
	if first.Code != http.StatusServiceUnavailable {
		t.Fatalf("first status = %d, want 503; body=%s", first.Code, first.Body.String())
	}
	if state := availability.CandidateState(route.channel.ID, route.upstreamModel, time.Now()); state != availability.StateCooldown {
		t.Fatalf("runtime state after provider failure = %v, want cooldown", state)
	}
	_ = runP4C1BCompact(t, route.group.Name, 4103)
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1; second request must be filtered by runtime cooldown", got)
	}
}

func TestP4C1BCompactIgnoresLegacyCircuitForCredentialAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writeP4C1BCompactSuccess(w)
	}))
	defer server.Close()

	route := newP4C1BCompactRoute(t, ctx, server.URL, "ignore-legacy-circuit", []model.ChannelKey{{Enabled: true, ChannelKey: "compact-key"}})
	key := route.channel.Keys[0]
	for i := 0; i < 5; i++ {
		balancer.RecordFailure(route.channel.ID, key.ID, route.upstreamModel, balancer.FailureHard)
	}
	if tripped, _ := balancer.IsTripped(route.channel.ID, key.ID, route.upstreamModel); !tripped {
		t.Fatal("test precondition: legacy circuit must be open")
	}

	recorder := runP4C1BCompact(t, route.group.Name, 4104)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; Compact admission must ignore legacy circuit; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}
}

func TestP4C1BWSRelaySkipsCredentialInAvailabilityCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)

	var gotAuth atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		writeP4C1BProviderUnavailable(w)
	}))
	defer server.Close()

	route := newP4C1BWSRoute(t, ctx, server.URL, "credential-cooldown", []model.ChannelKey{
		{Enabled: true, ChannelKey: "ws-key-one", TotalCost: 0},
		{Enabled: true, ChannelKey: "ws-key-two", TotalCost: 1},
	})
	first := route.channel.Keys[0]
	availability.RecordCredentialFailureRevision(route.channel.ID, first.ID, first.CredentialRevision, "test", time.Now())

	req, group := newP4C1BWSRelayRequest(t, ctx, route, 4201)
	_ = runWSRelay(ctx, req, group)
	if auth, _ := gotAuth.Load().(string); auth != "Bearer ws-key-two" {
		t.Fatalf("Authorization = %q, want second available credential", auth)
	}
}

func TestP4C1BWSRelayProviderFailureCreatesRuntimeCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeP4C1BProviderUnavailable(w)
	}))
	defer server.Close()

	route := newP4C1BWSRoute(t, ctx, server.URL, "provider-cooldown", []model.ChannelKey{{Enabled: true, ChannelKey: "ws-key"}})
	req, group := newP4C1BWSRelayRequest(t, ctx, route, 4202)
	result := runWSRelay(ctx, req, group)
	if result.Success {
		t.Fatal("expected upstream 503 to fail")
	}
	if state := availability.CandidateState(route.channel.ID, route.upstreamModel, time.Now()); state != availability.StateCooldown {
		t.Fatalf("runtime state after provider failure = %v, want cooldown", state)
	}
}

func TestP4C1BWSRelayIgnoresLegacyCircuitForCredentialAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writeP4C1BProviderUnavailable(w)
	}))
	defer server.Close()

	route := newP4C1BWSRoute(t, ctx, server.URL, "ignore-legacy-circuit", []model.ChannelKey{{Enabled: true, ChannelKey: "ws-key"}})
	key := route.channel.Keys[0]
	for i := 0; i < 5; i++ {
		balancer.RecordFailure(route.channel.ID, key.ID, route.upstreamModel, balancer.FailureHard)
	}
	if tripped, _ := balancer.IsTripped(route.channel.ID, key.ID, route.upstreamModel); !tripped {
		t.Fatal("test precondition: legacy circuit must be open")
	}

	req, group := newP4C1BWSRelayRequest(t, ctx, route, 4203)
	_ = runWSRelay(ctx, req, group)
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1; live WS admission must ignore legacy circuit", got)
	}
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
