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
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestRelayRequestCloneSharesControlContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(parent)
	control := newRelayControl(parent, LiveRequestSnapshot{RequestID: "lr_clone_control"})
	request := &relayRequest{c: c, control: control}

	clone := cloneRelayRequestForAttempt(request)
	if clone.control != control {
		t.Fatal("attempt clone must share the logical request control pointer")
	}
	if clone.requestContext() != control.Context() {
		t.Fatal("control context must be authoritative for cloned relay execution")
	}

	control.Interrupt()
	<-clone.requestContext().Done()
	if !errors.Is(context.Cause(clone.requestContext()), errManualInterrupt) {
		t.Fatalf("clone context cause = %v", context.Cause(clone.requestContext()))
	}
	if parent.Err() != nil {
		t.Fatalf("interrupting clone control canceled ingress parent: %v", parent.Err())
	}
}

func TestRelayRequestContextFallsBackToIngressWithoutControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parent := context.WithValue(context.Background(), struct{}{}, "ingress")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(parent)

	request := &relayRequest{c: c}
	if request.requestContext() != parent {
		t.Fatal("request without control must preserve existing ingress context semantics")
	}
}

func TestHTTPManualInterruptBeforeRunDoesNotReachUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbCtx := setupRelayTestDB(t)

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_pre","object":"response","created_at":1,"model":"gpt-4o","output":[],"status":"completed"}`))
	}))
	defer server.Close()

	groupName := "manual-interrupt-pre-dispatch-group"
	createManualInterruptHTTPGroup(t, dbCtx, groupName, []manualInterruptTestChannel{{
		name: "manual-interrupt-pre-dispatch", url: server.URL + "/v1", priority: 1,
	}})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 31)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":%q,"input":"hello"}`, groupName)))
	c.Request.Header.Set("Content-Type", "application/json")

	handler, ok := newRelayHandler(inbound.InboundTypeOpenAIResponse, c)
	if !ok {
		t.Fatalf("expected relay handler construction to succeed: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	defer handler.heartbeat.Stop()
	if handler.request.control == nil {
		t.Fatal("http relay handler must own a request-scoped control before execution")
	}

	handler.request.control.Interrupt()
	handler.run()

	if got := hits.Load(); got != 0 {
		t.Fatalf("manual interrupt before execution reached upstream %d times, want 0", got)
	}
	if c.Request.Context().Err() != nil {
		t.Fatalf("manual interrupt canceled ingress request context: %v", c.Request.Context().Err())
	}
}

func TestHTTPManualInterruptInFlightStopsWithoutProviderFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbCtx := setupRelayTestDB(t)

	var firstHits atomic.Int32
	var secondHits atomic.Int32
	firstStarted := make(chan struct{}, 1)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		select {
		case firstStarted <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_second","object":"response","created_at":1,"model":"gpt-4o","output":[],"status":"completed"}`))
	}))
	defer second.Close()

	groupName := "manual-interrupt-in-flight-group"
	createManualInterruptHTTPGroup(t, dbCtx, groupName, []manualInterruptTestChannel{
		{name: "manual-interrupt-first", url: first.URL + "/v1", priority: 1},
		{name: "manual-interrupt-second", url: second.URL + "/v1", priority: 2},
	})

	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 32)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":%q,"input":"hello"}`, groupName))).WithContext(parent)
	c.Request.Header.Set("Content-Type", "application/json")

	done := make(chan struct{})
	go func() {
		defer close(done)
		Handler(inbound.InboundTypeOpenAIResponse, c)
	}()

	select {
	case <-firstStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("first provider never received the request")
	}

	live := waitForHTTPManualInterruptSnapshot(t, groupName, 2*time.Second)
	if !InterruptLiveRequest(live.RequestID) {
		t.Fatalf("failed to interrupt active request %q", live.RequestID)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("relay did not stop after manual interrupt")
	}

	if got := firstHits.Load(); got != 1 {
		t.Fatalf("first provider hits=%d, want 1", got)
	}
	if got := secondHits.Load(); got != 0 {
		t.Fatalf("manual interrupt failed over to second provider %d times", got)
	}
	if parent.Err() != nil {
		t.Fatalf("manual interrupt canceled ingress parent: %v", parent.Err())
	}
	for _, snapshot := range ListLiveRequests() {
		if snapshot.RequestID == live.RequestID {
			t.Fatalf("completed interrupted request remained registered: %#v", snapshot)
		}
	}
}

type manualInterruptTestChannel struct {
	name     string
	url      string
	priority int
}

func createManualInterruptHTTPGroup(t *testing.T, ctx context.Context, groupName string, channels []manualInterruptTestChannel) {
	t.Helper()
	group := &dbmodel.Group{Name: groupName, Mode: dbmodel.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	for _, spec := range channels {
		channel := &dbmodel.Channel{
			Name:     spec.name,
			Type:     outbound.OutboundTypeOpenAIResponse,
			Enabled:  true,
			BaseUrls: []dbmodel.BaseUrl{{URL: spec.url}},
			Model:    "gpt-4o",
			Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
		}
		if err := op.ChannelCreate(channel, ctx); err != nil {
			t.Fatalf("ChannelCreate failed: %v", err)
		}
		if err := op.GroupItemAdd(&dbmodel.GroupItem{
			GroupID: group.ID, ChannelID: channel.ID, ModelName: "gpt-4o",
			Priority: spec.priority, Weight: 1,
		}, ctx); err != nil {
			t.Fatalf("GroupItemAdd failed: %v", err)
		}
	}
}

func waitForHTTPManualInterruptSnapshot(t *testing.T, requestedModel string, timeout time.Duration) LiveRequestSnapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, snapshot := range ListLiveRequests() {
			if snapshot.Transport == "http" && snapshot.RequestedModel == requestedModel {
				return snapshot
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("live http request for model %q did not appear", requestedModel)
	return LiveRequestSnapshot{}
}
