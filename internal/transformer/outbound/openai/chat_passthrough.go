package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// CanPassthrough reports whether the inbound request already uses the OpenAI
// Chat Completions wire format. Cross-format requests must keep using the
// standard InternalLLMRequest transformer path.
func (o *ChatOutbound) CanPassthrough(inboundFormat model.APIFormat) bool {
	return inboundFormat == model.APIFormatOpenAIChatCompletion
}

// TransformRequestRaw forwards a same-format Chat Completions request without
// rebuilding it through the explicit ChatCompletionsRequest whitelist. Only the
// selected upstream model and transport-owned request metadata are changed.
func (o *ChatOutbound) TransformRequestRaw(ctx context.Context, rawBody []byte, modelName, baseURL, key string, query url.Values) (*http.Request, error) {
	if len(rawBody) == 0 {
		return nil, fmt.Errorf("raw body is empty")
	}

	rewrittenBody, err := rewriteRawChatRequestModel(rawBody, modelName)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(rewrittenBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.ContentLength = int64(len(rewrittenBody))
	bodyBytes := rewrittenBody
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyBytes)), nil
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	parsedURL, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedURL.Path = parsedURL.Path + "/chat/completions"
	if query != nil {
		parsedURL.RawQuery = query.Encode()
	}
	req.URL = parsedURL
	req.Method = http.MethodPost

	return req, nil
}

// PassthroughConfig keeps Chat metrics collection on the existing sidecar
// parser. Chat Completions terminates streams with data: [DONE], which is not a
// typed SSE event name, so no TerminalEvents map is required here.
func (o *ChatOutbound) PassthroughConfig() model.PassthroughConfig {
	return model.PassthroughConfig{CollectMetrics: true}
}

type topLevelJSONFieldSpan struct {
	start int
	end   int
}

// rewriteRawChatRequestModel changes every top-level JSON field named "model"
// to the selected upstream model. With one unambiguous string-valued model that
// already matches, it returns the original bytes unchanged so field order and
// whitespace stay stable. Duplicate top-level model keys are deliberately
// normalized because leaving parser-dependent model values would let the wire
// request diverge from the model already selected by ZyRealm's control plane.
// Nested fields named "model" are never selected.
func rewriteRawChatRequestModel(rawBody []byte, modelName string) ([]byte, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return rawBody, nil
	}
	if !json.Valid(rawBody) {
		return nil, fmt.Errorf("failed to decode raw chat request: invalid JSON")
	}

	spans, ok := findTopLevelJSONFieldSpans(rawBody, "model")
	if !ok || len(spans) == 0 {
		return nil, fmt.Errorf("raw chat request is missing top-level model")
	}

	if len(spans) == 1 {
		span := spans[0]
		var currentModel string
		if err := json.Unmarshal(rawBody[span.start:span.end], &currentModel); err == nil && currentModel == modelName {
			return rawBody, nil
		}
	}

	encodedModel, err := json.Marshal(modelName)
	if err != nil {
		return nil, fmt.Errorf("failed to encode raw chat model: %w", err)
	}

	extraPerReplacement := len(encodedModel)
	capacity := len(rawBody) + len(spans)*extraPerReplacement
	result := make([]byte, 0, capacity)
	cursor := 0
	for _, span := range spans {
		result = append(result, rawBody[cursor:span.start]...)
		result = append(result, encodedModel...)
		cursor = span.end
	}
	result = append(result, rawBody[cursor:]...)
	return result, nil
}

// findTopLevelJSONFieldSpans returns the exact byte ranges of every top-level
// value for field. The scanner operates only on JSON-valid input and tracks
// strings/nesting so arrays or nested objects cannot be mistaken for top-level
// fields. Whitespace surrounding a value is intentionally left outside spans.
func findTopLevelJSONFieldSpans(raw []byte, field string) ([]topLevelJSONFieldSpan, bool) {
	i := skipJSONWhitespace(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		return nil, false
	}
	i++
	spans := make([]topLevelJSONFieldSpan, 0, 1)

	for {
		i = skipJSONWhitespace(raw, i)
		if i >= len(raw) {
			return nil, false
		}
		if raw[i] == '}' {
			return spans, true
		}
		if raw[i] != '"' {
			return nil, false
		}

		keyStart := i
		keyEnd, valid := scanJSONStringEnd(raw, keyStart)
		if !valid {
			return nil, false
		}
		var key string
		if err := json.Unmarshal(raw[keyStart:keyEnd], &key); err != nil {
			return nil, false
		}

		i = skipJSONWhitespace(raw, keyEnd)
		if i >= len(raw) || raw[i] != ':' {
			return nil, false
		}
		i = skipJSONWhitespace(raw, i+1)
		if i >= len(raw) {
			return nil, false
		}

		valueStart := i
		valueEnd, delimiter, valid := scanTopLevelJSONValueEnd(raw, valueStart)
		if !valid {
			return nil, false
		}
		trimmedEnd := valueEnd
		for trimmedEnd > valueStart && isJSONWhitespace(raw[trimmedEnd-1]) {
			trimmedEnd--
		}
		if trimmedEnd <= valueStart {
			return nil, false
		}
		if key == field {
			spans = append(spans, topLevelJSONFieldSpan{start: valueStart, end: trimmedEnd})
		}

		if delimiter == '}' {
			return spans, true
		}
		i = valueEnd + 1
	}
}

func scanTopLevelJSONValueEnd(raw []byte, start int) (end int, delimiter byte, ok bool) {
	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(raw); i++ {
		b := raw[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if b == '\\' {
				escaped = true
				continue
			}
			if b == '"' {
				inString = false
			}
			continue
		}

		switch b {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			if b == '}' && depth == 0 {
				return i, '}', true
			}
			if depth == 0 {
				return 0, 0, false
			}
			depth--
		case ',':
			if depth == 0 {
				return i, ',', true
			}
		}
	}
	return 0, 0, false
}

func scanJSONStringEnd(raw []byte, start int) (int, bool) {
	if start >= len(raw) || raw[start] != '"' {
		return 0, false
	}
	escaped := false
	for i := start + 1; i < len(raw); i++ {
		if escaped {
			escaped = false
			continue
		}
		switch raw[i] {
		case '\\':
			escaped = true
		case '"':
			return i + 1, true
		}
	}
	return 0, false
}

func skipJSONWhitespace(raw []byte, i int) int {
	for i < len(raw) && isJSONWhitespace(raw[i]) {
		i++
	}
	return i
}

func isJSONWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}
