package relay

import (
	"context"
	"net/http"
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
