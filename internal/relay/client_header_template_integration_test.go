package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/gin-gonic/gin"
)

func TestCopyHeadersRendersSafeClientHeaderTemplate(t *testing.T) {
	ra, outbound := newClientHeaderTemplateHTTPAttempt(t,
		http.Header{
			"OpenAI-Project":      []string{"project-a"},
			"OpenAI-Organization": []string{"org-a"},
		},
		[]dbmodel.CustomHeader{
			{HeaderKey: "X-Upstream-Project", HeaderValue: "tenant-{client_header:openai-project}"},
			{HeaderKey: "X-Upstream-Scope", HeaderValue: "{client_header:OpenAI-Organization}/{client_header:OpenAI-Project}"},
		},
		`{"model":"gpt-test","input":"keep"}`,
	)

	ra.copyHeaders(outbound)

	if got := outbound.Header.Get("X-Upstream-Project"); got != "tenant-project-a" {
		t.Fatalf("X-Upstream-Project=%q, want %q", got, "tenant-project-a")
	}
	if got := outbound.Header.Get("X-Upstream-Scope"); got != "org-a/project-a" {
		t.Fatalf("X-Upstream-Scope=%q, want %q", got, "org-a/project-a")
	}
}

func TestCopyHeadersSkipsTemplateWhenSourceIsMissing(t *testing.T) {
	ra, outbound := newClientHeaderTemplateHTTPAttempt(t,
		http.Header{},
		[]dbmodel.CustomHeader{{HeaderKey: "X-Upstream-Project", HeaderValue: "tenant-{client_header:OpenAI-Project}"}},
		`{"model":"gpt-test"}`,
	)

	ra.copyHeaders(outbound)

	if values, ok := outbound.Header["X-Upstream-Project"]; ok && len(values) > 0 {
		t.Fatalf("missing template source should omit X-Upstream-Project, got %q", values)
	}
}

func TestCopyHeadersNeverExposesBlockedClientCredentialTemplate(t *testing.T) {
	ra, outbound := newClientHeaderTemplateHTTPAttempt(t,
		http.Header{"Authorization": []string{"Bearer downstream-secret"}},
		[]dbmodel.CustomHeader{{HeaderKey: "X-Leak", HeaderValue: "{client_header:Authorization}"}},
		`{"model":"gpt-test"}`,
	)

	ra.copyHeaders(outbound)

	if values, ok := outbound.Header["X-Leak"]; ok && len(values) > 0 {
		t.Fatalf("blocked credential template must be omitted, got %q", values)
	}
}

func TestCopyHeadersKeepsJSONLookingTemplateValueOutOfBody(t *testing.T) {
	const rawBody = `{"model":"gpt-test","input":"keep"}`
	const metadata = `{"project":"a","scope":["x","y"]}`
	ra, outbound := newClientHeaderTemplateHTTPAttempt(t,
		http.Header{"X-Tenant-Meta": []string{metadata}},
		[]dbmodel.CustomHeader{{HeaderKey: "X-Upstream-Meta", HeaderValue: "{client_header:X-Tenant-Meta}"}},
		rawBody,
	)

	ra.copyHeaders(outbound)

	if got := outbound.Header.Get("X-Upstream-Meta"); got != metadata {
		t.Fatalf("X-Upstream-Meta=%q, want exact metadata %q", got, metadata)
	}
	body, err := io.ReadAll(outbound.Body)
	if err != nil {
		t.Fatalf("read outbound body: %v", err)
	}
	if got := string(body); got != rawBody {
		t.Fatalf("outbound body changed by header template: got %q want %q", got, rawBody)
	}
}

func TestCopyHeadersDoesNotRecursivelyExpandClientValue(t *testing.T) {
	ra, outbound := newClientHeaderTemplateHTTPAttempt(t,
		http.Header{"X-Tenant-ID": []string{"{client_header:Authorization}"}},
		[]dbmodel.CustomHeader{{HeaderKey: "X-Upstream-Tenant", HeaderValue: "{client_header:X-Tenant-ID}"}},
		`{"model":"gpt-test"}`,
	)

	ra.copyHeaders(outbound)

	if got := outbound.Header.Get("X-Upstream-Tenant"); got != "{client_header:Authorization}" {
		t.Fatalf("client value was recursively expanded or not rendered: got %q", got)
	}
}

func TestBuildUpstreamWSHeadersUsesRenderedChannelWithoutForwardingTemplateSource(t *testing.T) {
	channel := &dbmodel.Channel{CustomHeader: []dbmodel.CustomHeader{
		{HeaderKey: "X-Upstream-Project", HeaderValue: "tenant-{client_header:OpenAI-Project}"},
	}}
	clientHeaders := http.Header{"OpenAI-Project": []string{"project-a"}}
	renderedChannel := renderedChannelForTemplateSource(channel, clientHeaders)

	headers := buildUpstreamWSHeaders(nil, renderedChannel, "selected-key")

	if got := headers.Get("X-Upstream-Project"); got != "tenant-project-a" {
		t.Fatalf("X-Upstream-Project=%q, want %q", got, "tenant-project-a")
	}
	if got := headers.Get("OpenAI-Project"); got != "" {
		t.Fatalf("template source leaked into ordinary WS forwarding: %q", got)
	}
	if got := headers.Get("Authorization"); got != "Bearer selected-key" {
		t.Fatalf("Authorization=%q, want selected upstream key", got)
	}
	if got := headers.Get("OpenAI-Beta"); got != "responses_websockets=2026-02-06" {
		t.Fatalf("OpenAI-Beta=%q, want forced responses websocket beta", got)
	}
}

func TestRenderedCustomHeaderSeparatesWSPoolIdentity(t *testing.T) {
	channel := &dbmodel.Channel{CustomHeader: []dbmodel.CustomHeader{
		{HeaderKey: "X-Upstream-Tenant", HeaderValue: "{client_header:X-Tenant-ID}"},
	}}

	renderedA := renderedChannelForTemplateSource(channel, http.Header{"X-Tenant-ID": []string{"tenant-a"}})
	renderedB := renderedChannelForTemplateSource(channel, http.Header{"X-Tenant-ID": []string{"tenant-b"}})
	headersA := buildUpstreamWSHeaders(nil, renderedA, "same-key")
	headersB := buildUpstreamWSHeaders(nil, renderedB, "same-key")

	if got, want := headersA.Get("X-Upstream-Tenant"), "tenant-a"; got != want {
		t.Fatalf("tenant A custom header=%q, want %q", got, want)
	}
	if got, want := headersB.Get("X-Upstream-Tenant"), "tenant-b"; got != want {
		t.Fatalf("tenant B custom header=%q, want %q", got, want)
	}
	if wsHeaderSignature(headersA) == wsHeaderSignature(headersB) {
		t.Fatal("different rendered custom headers must produce different WS pool identities")
	}
}

func newClientHeaderTemplateHTTPAttempt(t *testing.T, source http.Header, custom []dbmodel.CustomHeader, body string) (*relayAttempt, *http.Request) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "http://client.test/v1/responses", strings.NewReader(body))
	for key, values := range source {
		for _, value := range values {
			c.Request.Header.Add(key, value)
		}
	}
	outbound := httptest.NewRequest(http.MethodPost, "https://upstream.test/v1/responses", strings.NewReader(body))
	return &relayAttempt{
		relayRequest: &relayRequest{c: c},
		channel:      &dbmodel.Channel{CustomHeader: custom},
	}, outbound
}
