package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type anthropicErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type anthropicSuccessEnvelope struct {
	Type    string                `json:"type"`
	ID      string                `json:"id"`
	Role    string                `json:"role"`
	Model   string                `json:"model"`
	Content json.RawMessage       `json:"content"`
	Error   *anthropicErrorDetail `json:"error,omitempty"`
}

// validateAnthropicSuccessResponse validates a non-streaming Anthropic response
// that arrived with an HTTP 2xx status. Some intermediate gateways incorrectly
// wrap their own error envelope in HTTP 200; forwarding that body as a successful
// Message makes Anthropic clients report a malformed/empty response and prevents
// the relay from trying another provider.
//
// The returned status is the semantic status the relay should use for routing.
// A well-formed Message returns its original success semantics (200). An Anthropic
// error envelope recovers the documented HTTP status from error.type. Any other
// malformed 2xx body is treated as a bad-gateway response.
func validateAnthropicSuccessResponse(body []byte) (int, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return http.StatusBadGateway, fmt.Errorf("invalid anthropic success response: empty body")
	}

	var envelope anthropicSuccessEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return http.StatusBadGateway, fmt.Errorf("invalid anthropic success response: invalid JSON: %w", err)
	}

	if strings.EqualFold(strings.TrimSpace(envelope.Type), "error") {
		status := anthropicErrorStatus(envelope.Error)
		errType := ""
		message := ""
		if envelope.Error != nil {
			errType = strings.TrimSpace(envelope.Error.Type)
			message = strings.TrimSpace(envelope.Error.Message)
		}
		return status, fmt.Errorf("anthropic error envelope returned with HTTP 2xx: type=%q message=%q", errType, message)
	}

	if strings.TrimSpace(envelope.Type) != "message" {
		return http.StatusBadGateway, fmt.Errorf("invalid anthropic success response: type=%q, want message", envelope.Type)
	}
	if strings.TrimSpace(envelope.ID) == "" {
		return http.StatusBadGateway, fmt.Errorf("invalid anthropic success response: missing message id")
	}
	if strings.TrimSpace(envelope.Role) != "assistant" {
		return http.StatusBadGateway, fmt.Errorf("invalid anthropic success response: role=%q, want assistant", envelope.Role)
	}
	if strings.TrimSpace(envelope.Model) == "" {
		return http.StatusBadGateway, fmt.Errorf("invalid anthropic success response: missing model")
	}

	content := bytes.TrimSpace(envelope.Content)
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return http.StatusBadGateway, fmt.Errorf("invalid anthropic success response: missing content array")
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(content, &blocks); err != nil {
		return http.StatusBadGateway, fmt.Errorf("invalid anthropic success response: content is not an array: %w", err)
	}

	return http.StatusOK, nil
}

func anthropicErrorStatus(detail *anthropicErrorDetail) int {
	if detail == nil {
		return http.StatusBadGateway
	}
	switch strings.TrimSpace(detail.Type) {
	case "invalid_request_error":
		return http.StatusBadRequest
	case "authentication_error":
		return http.StatusUnauthorized
	case "billing_error":
		return http.StatusPaymentRequired
	case "permission_error":
		return http.StatusForbidden
	case "not_found_error":
		return http.StatusNotFound
	case "conflict_error":
		return http.StatusConflict
	case "request_too_large":
		return http.StatusRequestEntityTooLarge
	case "rate_limit_error":
		return http.StatusTooManyRequests
	case "api_error":
		return http.StatusInternalServerError
	case "timeout_error":
		return http.StatusGatewayTimeout
	case "overloaded_error":
		return 529
	default:
		return http.StatusBadGateway
	}
}
