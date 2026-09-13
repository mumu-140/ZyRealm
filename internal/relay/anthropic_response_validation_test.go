package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/protocol"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/gin-gonic/gin"
)

func TestValidateAnthropicSuccessResponse(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantErr    bool
	}{
		{
			name:       "valid message",
			body:       `{"id":"msg_01","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "gateway error envelope hidden behind 200",
			body:       `{"code":400,"message":"channel failed"}`,
			wantStatus: http.StatusBadGateway,
			wantErr:    true,
		},
		{
			name:       "anthropic rate limit envelope hidden behind 200",
			body:       `{"type":"error","error":{"type":"rate_limit_error","message":"rate limit exceeded"},"request_id":"req_01"}`,
			wantStatus: http.StatusTooManyRequests,
			wantErr:    true,
		},
		{
			name:       "anthropic overloaded envelope hidden behind 200",
			body:       `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"},"request_id":"req_02"}`,
			wantStatus: 529,
			wantErr:    true,
		},
		{
			name:       "message missing id",
			body:       `{"type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[]}`,
			wantStatus: http.StatusBadGateway,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := validateAnthropicSuccessResponse([]byte(tt.body))
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d", status, tt.wantStatus)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandleResponsePassthroughRejectsMalformedAnthropic200BeforeCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	ra := &relayAttempt{
		relayRequest:     &relayRequest{c: c},
		channel:          &dbmodel.Channel{Name: "test-anthropic"},
		upstreamProtocol: protocol.Anthropic,
	}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(`{"code":400,"message":"channel failed"}`)),
	}

	status, err := ra.handleResponsePassthrough(context.Background(), response, transformerModel.PassthroughConfig{})
	if err == nil {
		t.Fatal("expected malformed Anthropic 200 response to fail")
	}
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", status, http.StatusBadGateway)
	}
	if c.Writer.Written() {
		t.Fatal("malformed upstream response must not commit downstream HTTP 200")
	}
	if ra.upstreamErrorBody == "" {
		t.Fatal("expected raw malformed body to be retained for routing diagnostics")
	}
}

func TestFallbackStatusPrefersSemanticFailureOverRawUpstream2xx(t *testing.T) {
	result := attemptResult{
		StatusCode:        http.StatusBadGateway,
		UpstreamStatus:    http.StatusOK,
		UpstreamErrorBody: `{"code":400,"message":"channel failed"}`,
		Err:               errors.New("invalid anthropic success response: type=\"\", want message"),
	}

	if got := fallbackStatus(result); got != http.StatusBadGateway {
		t.Fatalf("fallbackStatus = %d, want %d", got, http.StatusBadGateway)
	}
	if got := classifyRoutingFailure(result); got != failureDomainProviderTransient {
		t.Fatalf("classifyRoutingFailure = %v, want provider transient", got)
	}
}
