package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

type capturedChatPassthroughRequest struct {
	body          string
	authorization string
	path          string
	apiVersion    string
	trace         string
}

func TestHandlerOpenAIChatSameFormatUsesRawPassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	captured := make(chan capturedChatPassthroughRequest, 1)
	upstreamResponse := `{"id":"chatcmpl_passthrough","object":"chat.completion","created":1,"model":"chat-passthrough-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2},"future_response_field":{"survived":true}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream request body: %v", err)
			return
		}
		captured <- capturedChatPassthroughRequest{
			body:          string(body),
			authorization: r.Header.Get("Authorization"),
			path:          r.URL.Path,
			apiVersion:    r.URL.Query().Get("api-version"),
			trace:         r.URL.Query().Get("trace"),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(upstreamResponse))
	}))
	defer upstream.Close()

	channel := &dbmodel.Channel{
		Name:     "chat-passthrough-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL + "/v1"}},
		Model:    "chat-passthrough-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "upstream-secret"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	group := &dbmodel.Group{
		Name:         "chat-passthrough-model",
		Mode:         dbmodel.GroupModeFailover,
		RetryEnabled: true,
		MaxRetries:   1,
	}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: channel.ID, ModelName: "chat-passthrough-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add group item: %v", err)
	}

	rawRequest := `{"model":"chat-passthrough-model","messages":[{"role":"user","content":"hello"}],"future_field":{"mode":"new"},"nested":{"model":"inner-model"}}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions?api-version=2026-09-01&trace=abc",
		strings.NewReader(rawRequest),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer client-must-not-reach-upstream")
	Handler(inbound.InboundTypeOpenAIChat, c)

	var got capturedChatPassthroughRequest
	select {
	case got = <-captured:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not receive Chat request; passthrough/routing path did not dispatch")
	}
	if got.body != rawRequest {
		t.Fatalf("same-format Chat request was rebuilt instead of raw-passthrough:\n got: %s\nwant: %s", got.body, rawRequest)
	}
	if !strings.Contains(got.body, `"future_field":{"mode":"new"}`) {
		t.Fatalf("unknown future request field did not survive passthrough: %s", got.body)
	}
	if got.authorization != "Bearer upstream-secret" {
		t.Fatalf("upstream Authorization=%q want credential-owned bearer", got.authorization)
	}
	if got.path != "/v1/chat/completions" {
		t.Fatalf("upstream path=%q want /v1/chat/completions", got.path)
	}
	if got.apiVersion != "2026-09-01" || got.trace != "abc" {
		t.Fatalf("upstream query changed: api-version=%q trace=%q", got.apiVersion, got.trace)
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != upstreamResponse {
		t.Fatalf("same-format Chat response was rebuilt instead of raw-passthrough:\n got: %s\nwant: %s", recorder.Body.String(), upstreamResponse)
	}
	if !strings.Contains(recorder.Body.String(), `"future_response_field":{"survived":true}`) {
		t.Fatalf("unknown future response field did not survive passthrough: %s", recorder.Body.String())
	}
}
