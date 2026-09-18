package relay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/gin-gonic/gin"
)

func p5d1Source(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve P5D1 source path")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func p5d1EnableRetries(t *testing.T, ctx context.Context, groupID int, maxRetries int) {
	t.Helper()
	enabled := true
	if _, err := op.GroupUpdate(&model.GroupUpdateRequest{
		ID:           groupID,
		RetryEnabled: &enabled,
		MaxRetries:   &maxRetries,
	}, ctx); err != nil {
		t.Fatalf("enable retries for group %d: %v", groupID, err)
	}
}

func TestP5D1SidepathsUseDirectiveInsteadOfHTTPStatus(t *testing.T) {
	for _, name := range []string{"images.go", "compact.go", "ws_client.go"} {
		source := p5d1Source(t, name)
		if strings.Contains(source, "isRetryableStatus(") {
			t.Fatalf("%s still derives retry direction from HTTP status", name)
		}
		if !strings.Contains(source, "resolveSidepathDirective(") {
			t.Fatalf("%s does not consume the shared sidepath directive adapter", name)
		}
	}

	adapter := p5d1Source(t, "sidepath_disposition.go")
	for _, forbidden := range []string{
		"isRetryableStatus(",
		"fallbackStatus(",
		"classifyRoutingFailure(",
		"decideRoutingAttempt(",
		"withRoutingDecision(",
		"outlierErrorText(",
		"StatusCode",
		"Err",
	} {
		if strings.Contains(adapter, forbidden) {
			t.Fatalf("sidepath directive adapter must not reclassify through %q", forbidden)
		}
	}
}

func TestP5D1ImagesGeneric500RetriesSameCredential(t *testing.T) {
	ginTestMode(t)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var keyOneHits atomic.Int32
	var keyTwoHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer p5d1-image-key-one":
			keyOneHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"generic upstream failure"}}`)
		case "Bearer p5d1-image-key-two":
			keyTwoHits.Add(1)
			writeImagesSuccess(w)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	channel := newImagesTestChannel("p5d1-image-same-key", server.URL)
	channel.Keys = []model.ChannelKey{
		{Enabled: true, ChannelKey: "p5d1-image-key-one", TotalCost: 0},
		{Enabled: true, ChannelKey: "p5d1-image-key-two", TotalCost: 1},
	}
	group := &model.Group{
		Name:         "p5d1-image-same-key-group",
		Mode:         model.GroupModeFailover,
		RetryEnabled: true,
		MaxRetries:   1,
	}
	persistImagesRoute(t, ctx, group, channel)

	recorder, c := newImagesTestContext(
		"/v1/images/generations",
		[]byte(`{"model":"p5d1-image-same-key-group","prompt":"draw"}`),
		"application/json",
	)
	c.Set("api_key_id", 5601)
	ImagesHandler("/images/generations", c)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500 after same-credential retry; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := keyOneHits.Load(); got != 2 {
		t.Fatalf("first credential hits=%d, want 2", got)
	}
	if got := keyTwoHits.Load(); got != 0 {
		t.Fatalf("second credential hits=%d, want 0 for RETRY_SAME_CREDENTIAL", got)
	}
}

func TestP5D1ImagesProviderTransientLeavesProviderImmediately(t *testing.T) {
	ginTestMode(t)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var firstProviderHits atomic.Int32
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstProviderHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"upstream service temporarily unavailable"}}`)
	}))
	defer firstServer.Close()

	var secondProviderHits atomic.Int32
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondProviderHits.Add(1)
		writeImagesSuccess(w)
	}))
	defer secondServer.Close()

	first := newImagesTestChannel("p5d1-image-provider-a", firstServer.URL)
	first.Keys = []model.ChannelKey{
		{Enabled: true, ChannelKey: "p5d1-image-provider-a-key-1", TotalCost: 0},
		{Enabled: true, ChannelKey: "p5d1-image-provider-a-key-2", TotalCost: 1},
	}
	second := newImagesTestChannel("p5d1-image-provider-b", secondServer.URL)
	group := &model.Group{
		Name:         "p5d1-image-next-provider-group",
		Mode:         model.GroupModeFailover,
		RetryEnabled: true,
		MaxRetries:   1,
	}
	persistImagesRoute(t, ctx, group, first, second)

	recorder, c := newImagesTestContext(
		"/v1/images/generations",
		[]byte(`{"model":"p5d1-image-next-provider-group","prompt":"draw"}`),
		"application/json",
	)
	c.Set("api_key_id", 5602)
	ImagesHandler("/images/generations", c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 from second provider; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := firstProviderHits.Load(); got != 1 {
		t.Fatalf("first provider hits=%d, want 1 for NEXT_PROVIDER", got)
	}
	if got := secondProviderHits.Load(); got != 1 {
		t.Fatalf("second provider hits=%d, want 1", got)
	}
}

func TestP5D1CompactCredentialFailureRotatesWithinRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var badHits atomic.Int32
	var goodHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer p5d1-compact-bad":
			badHits.Add(1)
			writeP4C1BInvalidCredential(w)
		case "Bearer p5d1-compact-good":
			goodHits.Add(1)
			writeP4C1BCompactSuccess(w)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	route := newP4C1BCompactRoute(t, ctx, server.URL, "p5d1-rotate", []model.ChannelKey{
		{Enabled: true, ChannelKey: "p5d1-compact-bad", TotalCost: 0},
		{Enabled: true, ChannelKey: "p5d1-compact-good", TotalCost: 1},
	})
	p5d1EnableRetries(t, ctx, route.group.ID, 2)

	recorder := runP4C1BCompact(t, route.group.Name, 5603)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 after ROTATE_CREDENTIAL; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := badHits.Load(); got != 1 {
		t.Fatalf("bad credential hits=%d, want 1", got)
	}
	if got := goodHits.Load(); got != 1 {
		t.Fatalf("good credential hits=%d, want 1", got)
	}
}

func TestP5D1CompactProviderTransientLeavesProviderImmediately(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var firstHits atomic.Int32
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstHits.Add(1)
		writeP4C1BProviderUnavailable(w)
	}))
	defer firstServer.Close()

	var secondHits atomic.Int32
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondHits.Add(1)
		writeP4C1BCompactSuccess(w)
	}))
	defer secondServer.Close()

	route := newP4C1BCompactRoute(t, ctx, firstServer.URL, "p5d1-next-provider", []model.ChannelKey{{
		Enabled: true, ChannelKey: "p5d1-compact-provider-a",
	}})
	p5d1EnableRetries(t, ctx, route.group.ID, 2)

	second := &model.Channel{
		Name:      "p5d1-compact-provider-b",
		Type:      route.channel.Type,
		Enabled:   true,
		BaseUrls:  []model.BaseUrl{{URL: secondServer.URL + "/v1"}},
		Model:     route.upstreamModel,
		ProxyMode: model.ProxyUsageModeDirect,
		Keys:      []model.ChannelKey{{Enabled: true, ChannelKey: "p5d1-compact-provider-b-key"}},
	}
	if err := op.ChannelCreate(second, ctx); err != nil {
		t.Fatalf("create second compact provider: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{
		GroupID: route.group.ID, ChannelID: second.ID, ModelName: route.upstreamModel, Priority: 2, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add second compact provider: %v", err)
	}

	recorder := runP4C1BCompact(t, route.group.Name, 5604)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 from second provider; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := firstHits.Load(); got != 1 {
		t.Fatalf("first provider hits=%d, want 1 for NEXT_PROVIDER", got)
	}
	if got := secondHits.Load(); got != 1 {
		t.Fatalf("second provider hits=%d, want 1", got)
	}
}

func TestP5D1WSCredentialFailureRotatesWithinRound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var badHits atomic.Int32
	var goodHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer p5d1-ws-bad":
			badHits.Add(1)
			writeP4C1BInvalidCredential(w)
		case "Bearer p5d1-ws-good":
			goodHits.Add(1)
			writeP4C1BProviderUnavailable(w)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	route := newP4C1BWSRoute(t, ctx, server.URL, "p5d1-rotate", []model.ChannelKey{
		{Enabled: true, ChannelKey: "p5d1-ws-bad", TotalCost: 0},
		{Enabled: true, ChannelKey: "p5d1-ws-good", TotalCost: 1},
	})
	p5d1EnableRetries(t, ctx, route.group.ID, 2)

	req, group := newP4C1BWSRelayRequest(t, ctx, route, 5605)
	_ = runWSRelay(ctx, req, group)

	if got := badHits.Load(); got != 1 {
		t.Fatalf("bad credential hits=%d, want 1", got)
	}
	if got := goodHits.Load(); got != 1 {
		t.Fatalf("good credential hits=%d, want 1 after ROTATE_CREDENTIAL", got)
	}
}

func TestP5D1WSProviderTransientLeavesProviderImmediately(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writeP4C1BProviderUnavailable(w)
	}))
	defer server.Close()

	route := newP4C1BWSRoute(t, ctx, server.URL, "p5d1-next-provider", []model.ChannelKey{{
		Enabled: true, ChannelKey: "p5d1-ws-provider-key",
	}})
	p5d1EnableRetries(t, ctx, route.group.ID, 2)

	req, group := newP4C1BWSRelayRequest(t, ctx, route, 5606)
	_ = runWSRelay(ctx, req, group)

	if got := hits.Load(); got != 1 {
		t.Fatalf("provider hits=%d, want 1 for NEXT_PROVIDER", got)
	}
}
