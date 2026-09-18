package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
)

func TestImagesHandlerSkipsCredentialInAvailabilityCooldown(t *testing.T) {
	ginTestMode(t)
	ctx := setupRelayTestDB(t)

	var gotAuth atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		writeImagesSuccess(w)
	}))
	defer server.Close()

	channel := newImagesTestChannel("image-credential-cooldown", server.URL)
	channel.Keys = []model.ChannelKey{
		{Enabled: true, ChannelKey: "image-key-one", TotalCost: 0},
		{Enabled: true, ChannelKey: "image-key-two", TotalCost: 1},
	}
	group := &model.Group{Name: "public-image-credential-cooldown", Mode: model.GroupModeFailover}
	created := persistImagesRoute(t, ctx, group, channel)[0]
	if len(created.Keys) != 2 {
		t.Fatalf("created keys = %d, want 2", len(created.Keys))
	}
	first := created.Keys[0]
	availability.RecordCredentialFailureRevision(created.ID, first.ID, first.CredentialRevision, "test", time.Now())
	if availability.CredentialAvailableRevision(created.ID, first.ID, first.CredentialRevision, time.Now()) {
		t.Fatal("test precondition: first credential must be cooling")
	}

	recorder, c := newImagesTestContext(
		"/v1/images/generations",
		[]byte(`{"model":"public-image-credential-cooldown","prompt":"draw"}`),
		"application/json",
	)
	c.Set("api_key_id", 2001)
	ImagesHandler("/images/generations", c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if auth, _ := gotAuth.Load().(string); auth != "Bearer image-key-two" {
		t.Fatalf("Authorization = %q, want second available credential", auth)
	}
}

func TestImagesHandlerCredentialFailureRotatesAndPersistsCooldown(t *testing.T) {
	ginTestMode(t)
	ctx := setupRelayTestDB(t)

	var (
		mu    sync.Mutex
		auths []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		mu.Lock()
		auths = append(auths, auth)
		mu.Unlock()
		if auth == "Bearer image-key-one" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"Invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}`)
			return
		}
		writeImagesSuccess(w)
	}))
	defer server.Close()

	channel := newImagesTestChannel("image-credential-rotate", server.URL)
	channel.Keys = []model.ChannelKey{
		{Enabled: true, ChannelKey: "image-key-one", TotalCost: 0},
		{Enabled: true, ChannelKey: "image-key-two", TotalCost: 1},
	}
	group := &model.Group{
		Name:         "public-image-credential-rotate",
		Mode:         model.GroupModeFailover,
		RetryEnabled: true,
		MaxRetries:   2,
	}
	created := persistImagesRoute(t, ctx, group, channel)[0]
	first := created.Keys[0]

	recorder, c := newImagesTestContext(
		"/v1/images/generations",
		[]byte(`{"model":"public-image-credential-rotate","prompt":"draw"}`),
		"application/json",
	)
	c.Set("api_key_id", 2002)
	ImagesHandler("/images/generations", c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after credential rotation; body=%s", recorder.Code, recorder.Body.String())
	}
	mu.Lock()
	gotAuths := append([]string(nil), auths...)
	mu.Unlock()
	wantAuths := []string{"Bearer image-key-one", "Bearer image-key-two"}
	if fmt.Sprint(gotAuths) != fmt.Sprint(wantAuths) {
		t.Fatalf("authorization sequence = %v, want %v", gotAuths, wantAuths)
	}
	if availability.CredentialAvailableRevision(created.ID, first.ID, first.CredentialRevision, time.Now()) {
		t.Fatal("failed credential remained available; future image requests would retry a known-bad secret")
	}
}

func TestImagesHandlerProviderFailureCreatesRuntimeCooldown(t *testing.T) {
	ginTestMode(t)
	ctx := setupRelayTestDB(t)

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"upstream service temporarily unavailable"}}`)
	}))
	defer server.Close()

	channel := newImagesTestChannel("image-runtime-provider-cooldown", server.URL)
	group := &model.Group{Name: "public-image-runtime-provider-cooldown", Mode: model.GroupModeFailover}
	created := persistImagesRoute(t, ctx, group, channel)[0]

	makeRequest := func(apiKeyID int) *httptest.ResponseRecorder {
		recorder, c := newImagesTestContext(
			"/v1/images/generations",
			[]byte(`{"model":"public-image-runtime-provider-cooldown","prompt":"draw"}`),
			"application/json",
		)
		c.Set("api_key_id", apiKeyID)
		ImagesHandler("/images/generations", c)
		return recorder
	}

	first := makeRequest(2003)
	if first.Code != http.StatusServiceUnavailable {
		t.Fatalf("first status = %d, want 503; body=%s", first.Code, first.Body.String())
	}
	if state := availability.CandidateState(created.ID, "gpt-image-2", time.Now()); state != availability.StateCooldown {
		t.Fatalf("runtime state after provider failure = %v, want cooldown", state)
	}

	second := makeRequest(2004)
	if second.Code != http.StatusServiceUnavailable {
		t.Fatalf("second status = %d, want runtime rejection 503; body=%s", second.Code, second.Body.String())
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1; second request must be filtered by runtime cooldown", got)
	}
}

func TestImagesHandlerCommittedStreamFailureCreatesModelCooldownWithoutReplay(t *testing.T) {
	ginTestMode(t)
	ctx := setupRelayTestDB(t)

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: image_generation.partial_image\ndata: {\"type\":\"image_generation.partial_image\",\"partial_image_index\":0}\n\n")
	}))
	defer server.Close()

	channel := newImagesTestChannel("image-runtime-stream-failure", server.URL)
	group := &model.Group{
		Name:         "public-image-runtime-stream-failure",
		Mode:         model.GroupModeFailover,
		RetryEnabled: true,
		MaxRetries:   3,
	}
	created := persistImagesRoute(t, ctx, group, channel)[0]

	recorder, c := newImagesTestContext(
		"/v1/images/generations",
		[]byte(`{"model":"public-image-runtime-stream-failure","prompt":"draw","stream":true}`),
		"application/json",
	)
	c.Set("api_key_id", 2005)
	ImagesHandler("/images/generations", c)

	if recorder.Body.Len() == 0 {
		t.Fatal("committed stream failure did not forward the partial event")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1; committed response must never be replayed", got)
	}
	if state := availability.CandidateState(created.ID, "gpt-image-2", time.Now()); state != availability.StateCooldown {
		t.Fatalf("runtime state after committed stream failure = %v, want model cooldown", state)
	}
}

func TestImagesHandlerHalfOpenProbeIsSingleFlight(t *testing.T) {
	ginTestMode(t)
	ctx := setupRelayTestDB(t)

	var hits atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseProbe := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseProbe)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		writeImagesSuccess(w)
	}))
	defer server.Close()

	channel := newImagesTestChannel("image-half-open-single-flight", server.URL)
	channel.MaxConcurrency = 2
	group := &model.Group{Name: "public-image-half-open-single-flight", Mode: model.GroupModeFailover}
	created := persistImagesRoute(t, ctx, group, channel)[0]

	// Seed an already-expired provider cooldown. The next real request must be
	// the sole HALF_OPEN probe until it records success/failure or releases the lease.
	availability.RecordProviderFailure(created.ID, "test", time.Now().Add(-10*time.Second))
	if state := availability.CandidateState(created.ID, "gpt-image-2", time.Now()); state != availability.StateHalfOpen {
		t.Fatalf("test precondition: runtime state = %v, want half-open", state)
	}

	firstRecorder, firstCtx := newImagesTestContext(
		"/v1/images/generations",
		[]byte(`{"model":"public-image-half-open-single-flight","prompt":"first"}`),
		"application/json",
	)
	firstCtx.Set("api_key_id", 2007)
	firstDone := make(chan struct{})
	go func() {
		ImagesHandler("/images/generations", firstCtx)
		close(firstDone)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		releaseProbe()
		t.Fatal("half-open probe did not reach upstream")
	}

	secondRecorder, secondCtx := newImagesTestContext(
		"/v1/images/generations",
		[]byte(`{"model":"public-image-half-open-single-flight","prompt":"second"}`),
		"application/json",
	)
	secondCtx.Set("api_key_id", 2008)
	secondDone := make(chan struct{})
	go func() {
		ImagesHandler("/images/generations", secondCtx)
		close(secondDone)
	}()

	select {
	case <-secondDone:
	case <-time.After(time.Second):
		releaseProbe()
		<-firstDone
		t.Fatal("second request blocked instead of being rejected while the half-open lease was busy")
	}
	if got := hits.Load(); got != 1 {
		releaseProbe()
		<-firstDone
		t.Fatalf("upstream hits = %d, want 1 while half-open probe is in flight", got)
	}
	if secondRecorder.Code == http.StatusOK {
		releaseProbe()
		<-firstDone
		t.Fatalf("second request unexpectedly succeeded while half-open probe was in flight; body=%s", secondRecorder.Body.String())
	}

	releaseProbe()
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first half-open probe did not finish after release")
	}
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first probe status = %d, want 200; body=%s", firstRecorder.Code, firstRecorder.Body.String())
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("final upstream hits = %d, want exactly one", got)
	}
}
