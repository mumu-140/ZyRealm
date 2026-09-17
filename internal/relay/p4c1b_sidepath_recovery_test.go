package relay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/gin-gonic/gin"
)

func writeP4C1BInvalidCredential(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, `{"error":{"message":"Invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}`)
}

func TestP4C1BCompactPersistsCredentialCooldownAfter401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)

	var secondKeyHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer compact-bad-key":
			writeP4C1BInvalidCredential(w)
		case "Bearer compact-good-key":
			secondKeyHits.Add(1)
			writeP4C1BCompactSuccess(w)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	route := newP4C1BCompactRoute(t, ctx, server.URL, "credential-persist", []model.ChannelKey{
		{Enabled: true, ChannelKey: "compact-bad-key", TotalCost: 0},
		{Enabled: true, ChannelKey: "compact-good-key", TotalCost: 1},
	})
	badKey := route.channel.Keys[0]

	first := runP4C1BCompact(t, route.group.Name, 4401)
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("first status = %d, want 401; body=%s", first.Code, first.Body.String())
	}
	if availability.CredentialAvailableRevision(route.channel.ID, badKey.ID, badKey.CredentialRevision, time.Now()) {
		t.Fatal("invalid Compact credential remained available after canonical credential failure")
	}

	second := runP4C1BCompact(t, route.group.Name, 4402)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200 from the next credential; body=%s", second.Code, second.Body.String())
	}
	if got := secondKeyHits.Load(); got != 1 {
		t.Fatalf("second credential upstream hits = %d, want 1", got)
	}
}

func TestP4C1BWSRelayPersistsCredentialCooldownAfter401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)

	var badHits atomic.Int32
	var goodHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer ws-bad-key":
			badHits.Add(1)
			writeP4C1BInvalidCredential(w)
		case "Bearer ws-good-key":
			goodHits.Add(1)
			writeP4C1BProviderUnavailable(w)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	route := newP4C1BWSRoute(t, ctx, server.URL, "credential-persist", []model.ChannelKey{
		{Enabled: true, ChannelKey: "ws-bad-key", TotalCost: 0},
		{Enabled: true, ChannelKey: "ws-good-key", TotalCost: 1},
	})
	badKey := route.channel.Keys[0]

	req1, group1 := newP4C1BWSRelayRequest(t, ctx, route, 4501)
	_ = runWSRelay(ctx, req1, group1)
	if availability.CredentialAvailableRevision(route.channel.ID, badKey.ID, badKey.CredentialRevision, time.Now()) {
		t.Fatal("invalid WS credential remained available after canonical credential failure")
	}

	req2, group2 := newP4C1BWSRelayRequest(t, ctx, route, 4502)
	_ = runWSRelay(ctx, req2, group2)
	if got := badHits.Load(); got != 1 {
		t.Fatalf("bad credential hits = %d, want exactly 1 across two requests", got)
	}
	if got := goodHits.Load(); got != 1 {
		t.Fatalf("good credential hits = %d, want 1 on second request", got)
	}
}

func TestP4C1BCompactHalfOpenIsSingleFlight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)

	var hits atomic.Int32
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var startOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			startOnce.Do(func() { close(firstStarted) })
			<-releaseFirst
			writeP4C1BCompactSuccess(w)
			return
		}
		writeP4C1BProviderUnavailable(w)
	}))
	defer server.Close()

	route := newP4C1BCompactRoute(t, ctx, server.URL, "half-open-single-flight", []model.ChannelKey{{Enabled: true, ChannelKey: "compact-key"}})
	availability.RecordProviderFailure(route.channel.ID, "seed", time.Now().Add(-10*time.Second))
	if state := availability.CandidateState(route.channel.ID, route.upstreamModel, time.Now()); state != availability.StateHalfOpen {
		t.Fatalf("test precondition: state = %v, want half-open", state)
	}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_ = runP4C1BCompact(t, route.group.Name, 4601)
	}()
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first Compact half-open probe never reached upstream")
	}

	_ = runP4C1BCompact(t, route.group.Name, 4602)
	if got := hits.Load(); got != 1 {
		close(releaseFirst)
		<-firstDone
		t.Fatalf("upstream hits while first half-open probe is in flight = %d, want 1", got)
	}
	close(releaseFirst)
	<-firstDone
}

func TestP4C1BWSRelayHalfOpenIsSingleFlight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)

	var hits atomic.Int32
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var startOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			startOnce.Do(func() { close(firstStarted) })
			<-releaseFirst
		}
		writeP4C1BProviderUnavailable(w)
	}))
	defer server.Close()

	route := newP4C1BWSRoute(t, ctx, server.URL, "half-open-single-flight", []model.ChannelKey{{Enabled: true, ChannelKey: "ws-key"}})
	availability.RecordProviderFailure(route.channel.ID, "seed", time.Now().Add(-10*time.Second))
	if state := availability.CandidateState(route.channel.ID, route.upstreamModel, time.Now()); state != availability.StateHalfOpen {
		t.Fatalf("test precondition: state = %v, want half-open", state)
	}

	req1, group1 := newP4C1BWSRelayRequest(t, ctx, route, 4701)
	req2, group2 := newP4C1BWSRelayRequest(t, ctx, route, 4702)
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_ = runWSRelay(context.Background(), req1, group1)
	}()
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first WS half-open probe never reached upstream")
	}

	_ = runWSRelay(ctx, req2, group2)
	if got := hits.Load(); got != 1 {
		close(releaseFirst)
		<-firstDone
		t.Fatalf("upstream hits while first WS half-open probe is in flight = %d, want 1", got)
	}
	close(releaseFirst)
	<-firstDone
}
