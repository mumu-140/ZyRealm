package openai

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func chatPassthroughCapable(t *testing.T) model.PassthroughCapable {
	t.Helper()
	pt, ok := any(&ChatOutbound{}).(model.PassthroughCapable)
	if !ok {
		t.Fatal("ChatOutbound must implement model.PassthroughCapable")
	}
	return pt
}

func TestChatOutboundPassthroughCapabilityIsSameFormatOnly(t *testing.T) {
	pt := chatPassthroughCapable(t)
	if !pt.CanPassthrough(model.APIFormatOpenAIChatCompletion) {
		t.Fatal("OpenAI Chat inbound must be eligible for Chat same-format passthrough")
	}
	for _, format := range []model.APIFormat{
		model.APIFormatOpenAIResponse,
		model.APIFormatAnthropicMessage,
		model.APIFormatOpenAIEmbedding,
	} {
		if pt.CanPassthrough(format) {
			t.Fatalf("cross-format request %q must stay on the standard transformer path", format)
		}
	}
}

func TestChatOutboundTransformRequestRawPreservesIdenticalBodyAndReplayMetadata(t *testing.T) {
	pt := chatPassthroughCapable(t)
	raw := []byte("{\n  \"model\": \"gpt-5\",\n  \"messages\": [{\"role\":\"user\",\"content\":\"hi\"}],\n  \"future_field\": {\"enabled\": true},\n  \"nested\": {\"model\": \"must-not-change\"}\n}\n")
	query := url.Values{"api-version": {"2026-09-01"}, "trace": {"abc"}}

	req, err := pt.TransformRequestRaw(context.Background(), raw, "gpt-5", "https://example.test/v1", "upstream-secret", query)
	if err != nil {
		t.Fatalf("TransformRequestRaw: %v", err)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	if !bytes.Equal(body, raw) {
		t.Fatalf("same-model passthrough must preserve raw bytes exactly:\n got: %q\nwant: %q", body, raw)
	}
	if req.ContentLength != int64(len(raw)) {
		t.Fatalf("ContentLength=%d want %d", req.ContentLength, len(raw))
	}
	if req.GetBody == nil {
		t.Fatal("GetBody must be set so existing replay machinery can recreate the request body")
	}
	replayBody, err := req.GetBody()
	if err != nil {
		t.Fatalf("GetBody: %v", err)
	}
	defer replayBody.Close()
	replayed, err := io.ReadAll(replayBody)
	if err != nil {
		t.Fatalf("read replay body: %v", err)
	}
	if !bytes.Equal(replayed, raw) {
		t.Fatalf("GetBody changed raw request bytes: got %q want %q", replayed, raw)
	}
	if got := req.URL.Path; got != "/v1/chat/completions" {
		t.Fatalf("path=%q want /v1/chat/completions", got)
	}
	if got := req.URL.Query().Get("api-version"); got != "2026-09-01" {
		t.Fatalf("api-version query=%q", got)
	}
	if got := req.URL.Query().Get("trace"); got != "abc" {
		t.Fatalf("trace query=%q", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer upstream-secret" {
		t.Fatalf("Authorization=%q want credential-owned upstream bearer", got)
	}
}

func TestChatOutboundTransformRequestRawRewritesOnlyTopLevelModel(t *testing.T) {
	pt := chatPassthroughCapable(t)
	raw := []byte(`{"model":"client-alias","messages":[{"role":"user","content":"hi"}],"future_field":{"mode":"new"},"nested":{"model":"inner-model"}}`)

	req, err := pt.TransformRequestRaw(context.Background(), raw, "gpt-5.6", "https://example.test/v1", "key", nil)
	if err != nil {
		t.Fatalf("TransformRequestRaw: %v", err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}

	want := []byte(`{"model":"gpt-5.6","messages":[{"role":"user","content":"hi"}],"future_field":{"mode":"new"},"nested":{"model":"inner-model"}}`)
	if !bytes.Equal(body, want) {
		t.Fatalf("model alias rewrite must touch only the top-level model value:\n got: %s\nwant: %s", body, want)
	}
}

func TestChatOutboundTransformRequestRawNormalizesDuplicateTopLevelModels(t *testing.T) {
	pt := chatPassthroughCapable(t)
	raw := []byte(`{"nested":{"model":"inner-model"},"model":"ambiguous-first","messages":[{"role":"user","content":"hi"}],"model":"gpt-5.6","future_field":true}`)

	req, err := pt.TransformRequestRaw(context.Background(), raw, "gpt-5.6", "https://example.test/v1", "key", nil)
	if err != nil {
		t.Fatalf("TransformRequestRaw: %v", err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}

	want := []byte(`{"nested":{"model":"inner-model"},"model":"gpt-5.6","messages":[{"role":"user","content":"hi"}],"model":"gpt-5.6","future_field":true}`)
	if !bytes.Equal(body, want) {
		t.Fatalf("duplicate top-level model keys must be normalized to the selected upstream model without touching nested model fields:\n got: %s\nwant: %s", body, want)
	}
}
