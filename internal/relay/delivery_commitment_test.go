package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/gin-gonic/gin"
)

func TestDeliveryStartedIgnoresHTTPStreamHeartbeatBeforeModelPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	stream := true
	ra := &relayAttempt{relayRequest: &relayRequest{
		c:               c,
		internalRequest: &transformerModel.InternalLLMRequest{Stream: &stream},
	}}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.WriteHeader(http.StatusOK)
	if _, err := c.Writer.Write([]byte(":\n\n")); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
	c.Writer.Flush()
	if !c.Writer.Written() {
		t.Fatal("test setup requires the HTTP writer to be committed by the heartbeat")
	}

	if ra.deliveryStarted() {
		t.Fatal("heartbeat/header-only HTTP streaming output must not count as model delivery")
	}
}

func TestDeliveryStartedTreatsHTTPStreamModelPayloadAsCommitted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	stream := true
	ra := &relayAttempt{relayRequest: &relayRequest{
		c:               c,
		internalRequest: &transformerModel.InternalLLMRequest{Stream: &stream},
	}}
	ra.streamPayloadWritten.Store(true)

	if !ra.deliveryStarted() {
		t.Fatal("real HTTP streaming model payload must count as committed delivery")
	}
}

func TestDeliveryStartedPreservesNonStreamingHTTPWriterCommitment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	stream := false
	ra := &relayAttempt{relayRequest: &relayRequest{
		c:               c,
		internalRequest: &transformerModel.InternalLLMRequest{Stream: &stream},
	}}
	if _, err := c.Writer.Write([]byte("response")); err != nil {
		t.Fatalf("write response body: %v", err)
	}
	if !c.Writer.Written() {
		t.Fatal("test setup requires non-streaming HTTP output to commit the writer")
	}

	if !ra.deliveryStarted() {
		t.Fatal("non-streaming HTTP writer commitment must remain terminal")
	}
}
