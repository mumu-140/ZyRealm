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

func p5d2TrackAuthHandler(mu *sync.Mutex, hits map[string]int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.Header.Get("Authorization")]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"deliberate terminal request error","type":"invalid_request"}}`)
	}
}

func p5d2AssertEvenThreeKeyDistribution(t *testing.T, mu *sync.Mutex, hits map[string]int, prefix string) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	for _, suffix := range []string{"one", "two", "three"} {
		key := "Bearer " + prefix + suffix
		if got := hits[key]; got != 2 {
			t.Fatalf("distribution=%v; %s got %d, want 2", hits, key, got)
		}
	}
}

func p5d2Keys(prefix string) []model.ChannelKey {
	return []model.ChannelKey{
		{Enabled: true, ChannelKey: prefix + "one", TotalCost: 0},
		{Enabled: true, ChannelKey: prefix + "two", TotalCost: 1000},
		{Enabled: true, ChannelKey: prefix + "three", TotalCost: 1000000},
	}
}

func TestP5D2SidepathCredentialSelectionUsesFairScheduler(t *testing.T) {
	images := p5d2Source(t, "images.go")
	compact := p5d2Source(t, "compact.go")
	ws := p5d2Source(t, "ws_client.go")

	if !strings.Contains(images, "selectFairChannelCredential(") || strings.Contains(images, "channel.GetChannelKey(") {
		t.Fatal("Images live credential selection must use the fair scheduler")
	}
	if !strings.Contains(compact, "selectFairChannelCredential(") || strings.Contains(compact, "channel.GetChannelKey(") {
		t.Fatal("Compact live credential selection must use the fair scheduler")
	}
	if !strings.Contains(ws, "selectFairChannelCredential(") {
		t.Fatal("WS live credential selection must use the fair scheduler")
	}
	if !strings.Contains(ws, "peekFairChannelCredential(") {
		t.Fatal("WS warmup must use a non-charging fair preview")
	}
	if strings.Contains(ws, "channel.GetChannelKey(") {
		t.Fatal("WS credential selection must not fall back to TotalCost ordering")
	}

	fairSource := p5d2Source(t, filepath.Join("availability", "credential_fair.go"))
	if !strings.Contains(fairSource, "func PeekCredentialFair(") {
		t.Fatal("availability scheduler lacks non-charging fair preview")
	}
}

func TestP5D2ImagesSameCredentialRetryDoesNotRechargeFairLedger(t *testing.T) {
	images := p5d2Source(t, "images.go")

	selection := strings.Index(images, "selectFairChannelCredential(")
	retryLoop := strings.Index(images, "for upstreamStarts < maxUpstreamStarts {")
	if selection < 0 || retryLoop < 0 {
		t.Fatalf("missing Images fairness selection or retry loop")
	}
	if selection > retryLoop {
		t.Fatal("Images must charge fair credential selection before the retry loop so RETRY_SAME_CREDENTIAL reuses the current key")
	}

	sameStart := strings.Index(images, "case sidepathDirectiveRetrySameCredential:")
	rotateStart := strings.Index(images, "case sidepathDirectiveRotateCredential:")
	if sameStart < 0 || rotateStart < 0 || rotateStart <= sameStart {
		t.Fatal("missing Images sidepath retry directive branches")
	}
	sameBranch := images[sameStart:rotateStart]
	if strings.Contains(sameBranch, "selectFairChannelCredential(") || strings.Contains(sameBranch, "selectNextCredential(") {
		t.Fatal("Images RETRY_SAME_CREDENTIAL must not select or charge another credential")
	}
}

func TestP5D2WSRuntimeAdmissionPrecedesFairCredentialCharge(t *testing.T) {
	ws := p5d2Source(t, "ws_client.go")
	runStart := strings.Index(ws, "func runWSRelay(")
	runEnd := strings.Index(ws, "\nfunc finalizeWSRelay(")
	if runStart < 0 || runEnd < 0 || runEnd <= runStart {
		t.Fatal("cannot isolate runWSRelay")
	}
	runWS := ws[runStart:runEnd]

	admission := strings.Index(runWS, "availability.AcquireCandidate(")
	selection := strings.Index(runWS, "selectFairChannelCredential(")
	if admission < 0 || selection < 0 {
		t.Fatal("missing WS runtime admission or fair credential selection")
	}
	if selection < admission {
		t.Fatal("WS must pass runtime admission before charging the credential fairness ledger")
	}
}

func TestP5D2ImagesCredentialPoolIsEven(t *testing.T) {
	ginTestMode(t)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(p5d2TrackAuthHandler(&mu, hits))
	defer server.Close()

	channel := newImagesTestChannel("p5d2-images-fair", server.URL)
	channel.Keys = p5d2Keys("p5d2-images-")
	group := &model.Group{Name: "p5d2-images-fair-group", Mode: model.GroupModeFailover}
	persistImagesRoute(t, ctx, group, channel)

	for i := 0; i < 6; i++ {
		recorder, c := newImagesTestContext(
			"/v1/images/generations",
			[]byte(`{"model":"p5d2-images-fair-group","prompt":"draw"}`),
			"application/json",
		)
		c.Set("api_key_id", 5800+i)
		ImagesHandler("/images/generations", c)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("request %d status=%d, want 400; body=%s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	p5d2AssertEvenThreeKeyDistribution(t, &mu, hits, "p5d2-images-")
}

func TestP5D2CompactCredentialPoolIsEven(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(p5d2TrackAuthHandler(&mu, hits))
	defer server.Close()

	route := newP4C1BCompactRoute(t, ctx, server.URL, "p5d2-fair", p5d2Keys("p5d2-compact-"))
	for i := 0; i < 6; i++ {
		recorder := runP4C1BCompact(t, route.group.Name, 5900+i)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("request %d status=%d, want 400; body=%s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	p5d2AssertEvenThreeKeyDistribution(t, &mu, hits, "p5d2-compact-")
}

func TestP5D2WSCredentialPoolIsEven(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(p5d2TrackAuthHandler(&mu, hits))
	defer server.Close()

	route := newP4C1BWSRoute(t, ctx, server.URL, "p5d2-fair", p5d2Keys("p5d2-ws-"))
	for i := 0; i < 6; i++ {
		req, group := newP4C1BWSRelayRequest(t, context.Background(), route, 6000+i)
		result := runWSRelay(context.Background(), req, group)
		if result.Success {
			t.Fatalf("request %d unexpectedly succeeded", i+1)
		}
	}

	p5d2AssertEvenThreeKeyDistribution(t, &mu, hits, "p5d2-ws-")
}
