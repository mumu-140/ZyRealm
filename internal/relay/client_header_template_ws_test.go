package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
)

func TestWSTemplateSourceDoesNotBecomeOrdinaryForwardingHeaders(t *testing.T) {
	ctx := contextWithClientHeaderTemplateSource(context.Background(), http.Header{
		"OpenAI-Project": []string{"project-a"},
		"Authorization": []string{"Bearer downstream-secret"},
	})
	req := &relayRequest{
		ctx:                  ctx,
		templateHeaderSource: clientHeaderTemplateSourceFromContext(ctx),
	}
	ra := &relayAttempt{relayRequest: req}
	if got := ra.clientRequestHeaders(); got != nil {
		t.Fatalf("downstream WS ordinary forwarding unexpectedly gained headers: %#v", got)
	}

	channel := &dbmodel.Channel{CustomHeader: []dbmodel.CustomHeader{
		{HeaderKey: "X-Upstream-Project", HeaderValue: "{client_header:OpenAI-Project}"},
		{HeaderKey: "X-Leak", HeaderValue: "{client_header:Authorization}"},
	}}
	rendered := renderedChannelForTemplateSource(channel, req.clientHeaderTemplateSource())
	headers := buildUpstreamWSHeaders(ra.clientRequestHeaders(), rendered, "selected-key")

	if got := headers.Get("X-Upstream-Project"); got != "project-a" {
		t.Fatalf("rendered project=%q, want project-a", got)
	}
	if got := headers.Get("OpenAI-Project"); got != "" {
		t.Fatalf("template source leaked into ordinary WS forwarding: %q", got)
	}
	if got := headers.Get("X-Leak"); got != "" {
		t.Fatalf("blocked Authorization template leaked downstream secret: %q", got)
	}
	if got := headers.Get("Authorization"); got != "Bearer selected-key" {
		t.Fatalf("upstream Authorization=%q, want selected-key", got)
	}
}

func TestWSTemplateSourceNeverEntersPassthroughJSON(t *testing.T) {
	const rawBody = `{"model":"gpt-test","input":"keep"}`
	const metadata = `{"project":"tenant-a","scope":["x","y"]}`
	ctx := contextWithClientHeaderTemplateSource(context.Background(), http.Header{
		"X-Tenant-Meta": []string{metadata},
	})
	ra := &relayAttempt{relayRequest: &relayRequest{
		ctx:                  ctx,
		templateHeaderSource: clientHeaderTemplateSourceFromContext(ctx),
		rawBody:              []byte(rawBody),
	}}

	payload, err := ra.buildWSPassthroughRequestPayload()
	if err != nil {
		t.Fatalf("buildWSPassthroughRequestPayload: %v", err)
	}
	if !json.Valid(payload) {
		t.Fatalf("WS passthrough payload is not valid JSON: %s", payload)
	}
	if strings.Contains(string(payload), "tenant-a") || strings.Contains(string(payload), "scope") {
		t.Fatalf("template header metadata leaked into WS JSON payload: %s", payload)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("unmarshal WS payload: %v", err)
	}
	if got := string(body["input"]); got != `"keep"` {
		t.Fatalf("input changed while building WS payload: got %s", got)
	}
	if got := string(body["type"]); got != `"response.create"` {
		t.Fatalf("type=%s, want response.create", got)
	}
	if got := string(body["stream"]); got != "true" {
		t.Fatalf("stream=%s, want true", got)
	}
}
