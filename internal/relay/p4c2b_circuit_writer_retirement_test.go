package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func setP4C2BCircuitThresholdOne(t *testing.T) {
	t.Helper()
	if err := op.SettingSetInt(model.SettingKeyCircuitBreakerThreshold, 1); err != nil {
		t.Fatalf("SettingSetInt threshold failed: %v", err)
	}
}

func newP4C2BCoreRoute(t *testing.T, ctx context.Context, upstreamURL, name string) (*model.Channel, *model.Group) {
	t.Helper()
	channel := &model.Channel{
		Name:     name + "-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: upstreamURL + "/v1"}},
		Model:    name,
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: name + "-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	group := &model.Group{Name: name, Mode: model.GroupModeFailover, RetryEnabled: false}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{
		GroupID: group.ID, ChannelID: channel.ID, ModelName: name, Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}
	return channel, group
}

func runP4C2BCore(t *testing.T, modelName string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"`+modelName+`","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)
	return recorder
}

func TestP4C2BCoreFailureDoesNotWriteLegacyCircuit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	setP4C2BCircuitThresholdOne(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream boom"}}`))
	}))
	defer server.Close()

	channel, group := newP4C2BCoreRoute(t, ctx, server.URL, "p4c2b-core-failure")
	recorder := runP4C2BCore(t, group.Name)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	if balancer.PeekItemTripped(channel.ID, group.Name) {
		t.Fatal("core failure still wrote legacy circuit state")
	}
}

func TestP4C2BCoreSuccessDoesNotClearLegacyCircuit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	setP4C2BCircuitThresholdOne(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"chat.completion","created":1,"model":"p4c2b-core-success","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	channel, group := newP4C2BCoreRoute(t, ctx, server.URL, "p4c2b-core-success")
	key := channel.Keys[0]
	balancer.RecordFailure(channel.ID, key.ID, group.Name, balancer.FailureHard)
	if !balancer.PeekItemTripped(channel.ID, group.Name) {
		t.Fatal("test precondition: legacy circuit must be open")
	}

	recorder := runP4C2BCore(t, group.Name)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if !balancer.PeekItemTripped(channel.ID, group.Name) {
		t.Fatal("core success still cleared legacy circuit state")
	}
}

func TestP4C2BImagesFailureDoesNotWriteLegacyCircuit(t *testing.T) {
	ginTestMode(t)
	ctx := setupRelayTestDB(t)
	setP4C2BCircuitThresholdOne(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream service temporarily unavailable"}}`))
	}))
	defer server.Close()

	channel := newImagesTestChannel("p4c2b-images-failure", server.URL)
	group := &model.Group{Name: "p4c2b-images-failure-group", Mode: model.GroupModeFailover}
	created := persistImagesRoute(t, ctx, group, channel)[0]

	recorder, c := newImagesTestContext(
		"/v1/images/generations",
		[]byte(`{"model":"p4c2b-images-failure-group","prompt":"draw"}`),
		"application/json",
	)
	ImagesHandler("/images/generations", c)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}
	if balancer.PeekItemTripped(created.ID, "gpt-image-2") {
		t.Fatal("Images failure still wrote legacy circuit state")
	}
}

func TestP4C2BImagesSuccessDoesNotClearLegacyCircuit(t *testing.T) {
	ginTestMode(t)
	ctx := setupRelayTestDB(t)
	setP4C2BCircuitThresholdOne(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeImagesSuccess(w)
	}))
	defer server.Close()

	channel := newImagesTestChannel("p4c2b-images-success", server.URL)
	group := &model.Group{Name: "p4c2b-images-success-group", Mode: model.GroupModeFailover}
	created := persistImagesRoute(t, ctx, group, channel)[0]
	key := created.Keys[0]
	balancer.RecordFailure(created.ID, key.ID, "gpt-image-2", balancer.FailureHard)
	if !balancer.PeekItemTripped(created.ID, "gpt-image-2") {
		t.Fatal("test precondition: legacy circuit must be open")
	}

	recorder, c := newImagesTestContext(
		"/v1/images/generations",
		[]byte(`{"model":"p4c2b-images-success-group","prompt":"draw"}`),
		"application/json",
	)
	ImagesHandler("/images/generations", c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if !balancer.PeekItemTripped(created.ID, "gpt-image-2") {
		t.Fatal("Images success still cleared legacy circuit state")
	}
}

func TestP4C2BCompactFailureDoesNotWriteLegacyCircuit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	setP4C2BCircuitThresholdOne(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeP4C1BProviderUnavailable(w)
	}))
	defer server.Close()

	route := newP4C1BCompactRoute(t, ctx, server.URL, "p4c2b-failure", []model.ChannelKey{{Enabled: true, ChannelKey: "compact-key"}})
	recorder := runP4C1BCompact(t, route.group.Name, 5101)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}
	if balancer.PeekItemTripped(route.channel.ID, route.upstreamModel) {
		t.Fatal("Compact failure still wrote legacy circuit state")
	}
}

func TestP4C2BCompactSuccessDoesNotClearLegacyCircuit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	setP4C2BCircuitThresholdOne(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeP4C1BCompactSuccess(w)
	}))
	defer server.Close()

	route := newP4C1BCompactRoute(t, ctx, server.URL, "p4c2b-success", []model.ChannelKey{{Enabled: true, ChannelKey: "compact-key"}})
	key := route.channel.Keys[0]
	balancer.RecordFailure(route.channel.ID, key.ID, route.upstreamModel, balancer.FailureHard)
	if !balancer.PeekItemTripped(route.channel.ID, route.upstreamModel) {
		t.Fatal("test precondition: legacy circuit must be open")
	}

	recorder := runP4C1BCompact(t, route.group.Name, 5102)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if !balancer.PeekItemTripped(route.channel.ID, route.upstreamModel) {
		t.Fatal("Compact success still cleared legacy circuit state")
	}
}

func TestP4C2BWSFailureDoesNotWriteLegacyCircuit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	setP4C2BCircuitThresholdOne(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeP4C1BProviderUnavailable(w)
	}))
	defer server.Close()

	route := newP4C1BWSRoute(t, ctx, server.URL, "p4c2b-failure", []model.ChannelKey{{Enabled: true, ChannelKey: "ws-key"}})
	req, group := newP4C1BWSRelayRequest(t, ctx, route, 5201)
	result := runWSRelay(ctx, req, group)
	if result.Success {
		t.Fatal("expected upstream 503 to fail")
	}
	if balancer.PeekItemTripped(route.channel.ID, route.upstreamModel) {
		t.Fatal("WebSocket failure still wrote legacy circuit state")
	}
}
