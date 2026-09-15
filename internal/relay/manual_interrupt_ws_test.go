package relay

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/coder/websocket"
)

func TestWSManualInterruptOnlyStopsCurrentResponseCreateRound(t *testing.T) {
	ginTestMode(t)
	dbCtx := setupRelayTestDB(t)
	if err := op.SettingSetString(model.SettingKeyResponsesWSEnabled, "false"); err != nil {
		t.Fatalf("disable upstream responses ws: %v", err)
	}

	var hits atomic.Int32
	firstStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit := hits.Add(1)
		if hit == 1 {
			select {
			case firstStarted <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_ws_round_2","object":"response","model":"gpt-4o","created_at":1,"output":[],"status":"in_progress"}}`,
			"",
			`data: {"type":"response.output_text.delta","response_id":"resp_ws_round_2","delta":"ok"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_ws_round_2","object":"response","model":"gpt-4o","created_at":1,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
			"",
		}, "\n"))
	}))
	defer func() {
		close(release)
		upstream.Close()
	}()

	groupName := "manual-interrupt-ws-round-group"
	channel := &model.Channel{
		Name:     "manual-interrupt-ws-round-channel",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: upstream.URL + "/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
	}
	if err := op.ChannelCreate(channel, dbCtx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	group := &model.Group{Name: groupName, Mode: model.GroupModeFailover, SessionKeepTime: 60}
	if err := op.GroupCreate(group, dbCtx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "gpt-4o", Priority: 1, Weight: 1}, dbCtx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	clientConn, serverConn := newTestWSConnPair(t)
	defer clientConn.Close(websocket.StatusNormalClosure, "")
	defer serverConn.Close(websocket.StatusNormalClosure, "")

	connectionCtx, connectionCancel := context.WithCancel(context.Background())
	defer connectionCancel()
	const apiKeyID = 71
	const downstreamSessionID = "ws_manual_interrupt_round_scope"

	firstDone := make(chan *wsConversationState, 1)
	go func() {
		firstDone <- processWSResponseCreate(
			connectionCtx,
			serverConn,
			[]byte(fmt.Sprintf(`{"type":"response.create","model":%q,"input":"first"}`, groupName)),
			apiKeyID,
			"",
			downstreamSessionID,
			nil,
		)
	}()

	select {
	case <-firstStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("first ws round never reached upstream")
	}

	live := waitForWSManualInterruptSnapshot(t, groupName, 2*time.Second)
	if !InterruptLiveRequest(live.RequestID) {
		t.Fatalf("failed to interrupt active ws round %q", live.RequestID)
	}

	var state *wsConversationState
	select {
	case state = <-firstDone:
	case <-time.After(3 * time.Second):
		t.Fatal("first ws round did not stop after manual interrupt")
	}
	if connectionCtx.Err() != nil {
		t.Fatalf("manual interrupt canceled downstream websocket connection context: %v", connectionCtx.Err())
	}

	secondDone := make(chan *wsConversationState, 1)
	go func() {
		secondDone <- processWSResponseCreate(
			connectionCtx,
			serverConn,
			[]byte(fmt.Sprintf(`{"type":"response.create","model":%q,"input":"second"}`, groupName)),
			apiKeyID,
			"",
			downstreamSessionID,
			state,
		)
	}()

	readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer readCancel()
	completed := false
	for !completed {
		_, data, err := clientConn.Read(readCtx)
		if err != nil {
			t.Fatalf("downstream websocket closed before second round completed: %v", err)
		}
		if strings.Contains(string(data), `"type":"response.completed"`) && strings.Contains(string(data), "resp_ws_round_2") {
			completed = true
		}
	}

	select {
	case <-secondDone:
	case <-time.After(3 * time.Second):
		t.Fatal("second ws round did not finish")
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("upstream hits=%d, want exactly two rounds", got)
	}
}

func waitForWSManualInterruptSnapshot(t *testing.T, requestedModel string, timeout time.Duration) LiveRequestSnapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, snapshot := range ListLiveRequests() {
			if snapshot.Transport == "ws" && snapshot.RequestedModel == requestedModel {
				return snapshot
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("live ws round for model %q did not appear", requestedModel)
	return LiveRequestSnapshot{}
}
