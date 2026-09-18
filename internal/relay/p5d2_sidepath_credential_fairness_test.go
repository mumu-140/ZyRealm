package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/gin-gonic/gin"
)

func p5d2Source(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve P5D2 source path")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func p5d2FunctionBody(t *testing.T, source, startMarker, endMarker string) string {
	t.Helper()
	start := strings.Index(source, startMarker)
	if start < 0 {
		t.Fatalf("missing function start %q", startMarker)
	}
	end := strings.Index(source[start+len(startMarker):], endMarker)
	if end < 0 {
		t.Fatalf("missing function end %q", endMarker)
	}
	return source[start : start+len(startMarker)+end]
}

func p5d2Between(t *testing.T, source, startMarker, endMarker string) string {
	t.Helper()
	start := strings.Index(source, startMarker)
	if start < 0 {
		t.Fatalf("missing segment start %q", startMarker)
	}
	relEnd := strings.Index(source[start+len(startMarker):], endMarker)
	if relEnd < 0 {
		t.Fatalf("missing segment end %q", endMarker)
	}
	return source[start : start+len(startMarker)+relEnd]
}

func p5d2CountHits(t *testing.T, hits map[string]int, mu *sync.Mutex, keys ...string) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	for _, key := range keys {
		if got := hits["Bearer "+key]; got != 2 {
			t.Fatalf("credential distribution=%v; %s got %d, want 2", hits, key, got)
		}
	}
}

func TestP5D2LiveSidepathsUseFairCredentialSelection(t *testing.T) {
	images := p5d2Source(t, "images.go")
	compact := p5d2Source(t, "compact.go")
	ws := p5d2Source(t, "ws_client.go")

	if !strings.Contains(images, "selectFairChannelCredential(") {
		t.Fatal("Images live routing does not use provider-local credential fairness")
	}
	if strings.Contains(images, "channel.GetChannelKey(") {
		t.Fatal("Images live routing still uses TotalCost credential selection")
	}
	if !strings.Contains(compact, "selectFairChannelCredential(") {
		t.Fatal("Compact live routing does not use provider-local credential fairness")
	}
	if strings.Contains(compact, "channel.GetChannelKey(") {
		t.Fatal("Compact live routing still uses TotalCost credential selection")
	}

	runWS := p5d2FunctionBody(t, ws, "func runWSRelay(", "\nfunc finalizeWSRelay(")
	if !strings.Contains(runWS, "selectFairChannelCredential(") {
		t.Fatal("WS live routing does not use provider-local credential fairness")
	}
	if strings.Contains(runWS, "channel.GetChannelKey(") {
		t.Fatal("WS live routing still uses TotalCost credential selection")
	}

	for name, source := range map[string]string{
		"Images": images,
		"Compact": compact,
		"WebSocket": runWS,
	} {
		sameRetry := p5d2Between(t, source, "case sidepathDirectiveRetrySameCredential:", "case sidepathDirectiveRotateCredential:")
		if strings.Contains(sameRetry, "selectNextCredential(") || strings.Contains(sameRetry, "selectFairChannelCredential(") {
			t.Fatalf("%s same-credential retry must reuse the current key without charging another fair allocation", name)
		}
	}

	warmup := p5d2FunctionBody(t, ws, "func bestEffortWarmupUpstreamWS(", "\nfunc extractWSRequestModel(")
	if !strings.Contains(warmup, "channel.GetChannelKey(") {
		t.Fatal("WS warmup must retain non-accounting credential selection")
	}
	if strings.Contains(warmup, "selectFairChannelCredential(") {
		t.Fatal("WS warmup must not consume the live fair ledger")
	}
}

func TestP5D2ImagesCredentialDistributionIgnoresTotalCost(t *testing.T) {
	ginTestMode(t)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.Header.Get("Authorization")]++
		mu.Unlock()
		writeImagesSuccess(w)
	}))
	defer server.Close()

	channel := newImagesTestChannel("p5d2-images-fair", server.URL)
	channel.Keys = []model.ChannelKey{
		{Enabled: true, ChannelKey: "p5d2-image-key-1", TotalCost: 0},
		{Enabled: true, ChannelKey: "p5d2-image-key-2", TotalCost: 1000},
		{Enabled: true, ChannelKey: "p5d2-image-key-3", TotalCost: 1000000},
	}
	group := &model.Group{Name: "p5d2-images-fair-group", Mode: model.GroupModeFailover}
	persistImagesRoute(t, ctx, group, channel)

	for i := 0; i < 6; i++ {
		recorder, c := newImagesTestContext(
			"/v1/images/generations",
			[]byte(`{"model":"p5d2-images-fair-group","prompt":"draw"}`),
			"application/json",
		)
		c.Set("api_key_id", 5700+i)
		ImagesHandler("/images/generations", c)
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d status=%d, want 200; body=%s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	p5d2CountHits(t, hits, &mu, "p5d2-image-key-1", "p5d2-image-key-2", "p5d2-image-key-3")
}

func TestP5D2CompactCredentialDistributionIgnoresTotalCost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.Header.Get("Authorization")]++
		mu.Unlock()
		writeP4C1BCompactSuccess(w)
	}))
	defer server.Close()

	route := newP4C1BCompactRoute(t, ctx, server.URL, "p5d2-fair", []model.ChannelKey{
		{Enabled: true, ChannelKey: "p5d2-compact-key-1", TotalCost: 0},
		{Enabled: true, ChannelKey: "p5d2-compact-key-2", TotalCost: 1000},
		{Enabled: true, ChannelKey: "p5d2-compact-key-3", TotalCost: 1000000},
	})

	for i := 0; i < 6; i++ {
		recorder := runP4C1BCompact(t, route.group.Name, 5800+i)
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d status=%d, want 200; body=%s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	p5d2CountHits(t, hits, &mu, "p5d2-compact-key-1", "p5d2-compact-key-2", "p5d2-compact-key-3")
}

func TestP5D2WSCredentialDistributionIgnoresTotalCost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.Header.Get("Authorization")]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"generic upstream failure"}}`)
	}))
	defer server.Close()

	route := newP4C1BWSRoute(t, ctx, server.URL, "p5d2-fair", []model.ChannelKey{
		{Enabled: true, ChannelKey: "p5d2-ws-key-1", TotalCost: 0},
		{Enabled: true, ChannelKey: "p5d2-ws-key-2", TotalCost: 1000},
		{Enabled: true, ChannelKey: "p5d2-ws-key-3", TotalCost: 1000000},
	})

	for i := 0; i < 6; i++ {
		req, group := newP4C1BWSRelayRequest(t, ctx, route, 5900+i)
		_ = runWSRelay(ctx, req, group)
	}

	p5d2CountHits(t, hits, &mu, "p5d2-ws-key-1", "p5d2-ws-key-2", "p5d2-ws-key-3")
}
