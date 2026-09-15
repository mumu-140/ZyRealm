package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestLiveRequestSnapshotTracksActiveWireAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbCtx := setupRelayTestDB(t)

	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer server.Close()

	groupName := "live-runtime-metadata-group"
	createManualInterruptHTTPGroup(t, dbCtx, groupName, []manualInterruptTestChannel{{
		name: "live-runtime-metadata", url: server.URL + "/v1", priority: 1,
	}})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 41)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":%q,"input":"hello"}`, groupName)))
	c.Request.Header.Set("Content-Type", "application/json")

	done := make(chan struct{})
	go func() {
		defer close(done)
		Handler(inbound.InboundTypeOpenAIResponse, c)
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request did not start")
	}

	snapshot := waitForHTTPManualInterruptSnapshot(t, groupName, 2*time.Second)
	if snapshot.ChannelID == 0 || snapshot.ChannelKeyID == 0 {
		t.Fatalf("active snapshot is missing channel/key identity: %#v", snapshot)
	}
	if snapshot.UpstreamProtocol != "openai_response" {
		t.Fatalf("upstream protocol=%q, want openai_response", snapshot.UpstreamProtocol)
	}
	if snapshot.ProviderAttempt != 1 || snapshot.WireAttempt != 1 {
		t.Fatalf("attempt indexes provider=%d wire=%d, want 1/1", snapshot.ProviderAttempt, snapshot.WireAttempt)
	}
	if snapshot.Phase != string(livePhaseAttempting) {
		t.Fatalf("phase=%q, want attempting", snapshot.Phase)
	}
	if snapshot.DispatchState != dispatchStateString(dispatchMaybeSent) {
		t.Fatalf("dispatch=%q, want maybe_sent", snapshot.DispatchState)
	}

	if !InterruptLiveRequest(snapshot.RequestID) {
		t.Fatalf("failed to interrupt request %q", snapshot.RequestID)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("relay did not stop after metadata assertion interrupt")
	}
}

func TestRunProtocolRetriesPreservesManualInterruptCauseDuringBackoff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbCtx := setupRelayTestDB(t)

	var hits atomic.Int32
	firstServed := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"temporary upstream failure"}}`))
			select {
			case firstServed <- struct{}{}:
			default:
			}
			return
		}
		_, _ = w.Write([]byte(`{"id":"resp_retry","object":"response","created_at":1,"model":"gpt-4o","output":[],"status":"completed"}`))
	}))
	defer server.Close()

	groupName := "manual-interrupt-backoff-group"
	group := &dbmodel.Group{Name: groupName, Mode: dbmodel.GroupModeFailover, RetryEnabled: true, MaxRetries: 2}
	if err := op.GroupCreate(group, dbCtx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	channel := &dbmodel.Channel{
		Name: "manual-interrupt-backoff", Type: outbound.OutboundTypeOpenAIResponse, Enabled: true,
		BaseUrls: []dbmodel.BaseUrl{{URL: server.URL + "/v1"}}, Model: "gpt-4o",
		Keys: []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
	}
	if err := op.ChannelCreate(channel, dbCtx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "gpt-4o", Priority: 1, Weight: 1}, dbCtx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 42)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":%q,"input":"hello"}`, groupName)))
	c.Request.Header.Set("Content-Type", "application/json")

	handler, ok := newRelayHandler(inbound.InboundTypeOpenAIResponse, c)
	if !ok {
		t.Fatalf("newRelayHandler failed: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	defer handler.heartbeat.Stop()
	if !handler.iterator.Next() {
		t.Fatal("expected one routing candidate")
	}
	item := handler.iterator.Item()
	selectedChannel, err := op.ChannelGet(item.ChannelID, handler.request.requestContext())
	if err != nil {
		t.Fatalf("ChannelGet failed: %v", err)
	}
	upstreamModel := balancer.ItemUpstreamModel(item, handler.request.requestModel)
	legacyEligible, _ := legacyChannelEligibility(selectedChannel, handler.request.internalRequest, handler.passthroughRequired)
	key, plans := handler.selectCandidateAttempt(selectedChannel, upstreamModel, legacyEligible, map[int]struct{}{})
	if key.ChannelKey == "" || len(plans) == 0 {
		t.Fatal("expected a selected key and protocol plan")
	}

	resultCh := make(chan attemptResult, 1)
	go func() {
		resultCh <- runProtocolRetries(handler.request.requestContext(), handler.request, selectedChannel, key, plans[0], handler.group.FirstTokenTimeOut, 2)
	}()

	select {
	case <-firstServed:
	case <-time.After(3 * time.Second):
		t.Fatal("first retryable upstream response never completed")
	}
	// Allow the relay to enter retry backoff; the backoff itself is much longer
	// than this bounded synchronization delay.
	time.Sleep(50 * time.Millisecond)
	handler.request.control.Interrupt()

	var result attemptResult
	select {
	case result = <-resultCh:
	case <-time.After(3 * time.Second):
		t.Fatal("runProtocolRetries did not stop after manual interrupt")
	}
	if !errors.Is(result.Err, errManualInterrupt) {
		t.Fatalf("backoff result lost manual cause: %v", result.Err)
	}
	if result.Decision.RuleID != "manual_interrupt" || !result.Decision.Terminal {
		t.Fatalf("backoff routing decision=%#v", result.Decision)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("manual interrupt during backoff allowed %d upstream attempts, want 1", got)
	}
	if context.Cause(handler.request.requestContext()) != errManualInterrupt {
		t.Fatalf("control context cause=%v", context.Cause(handler.request.requestContext()))
	}
}
