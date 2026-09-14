package headerutil

import (
	"net/http"
	"testing"
)

func TestRenderClientHeaderTemplate(t *testing.T) {
	source := http.Header{
		"Openai-Project":      []string{"project-a"},
		"Openai-Organization": []string{"org-a"},
	}

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "literal", value: "literal-value", want: "literal-value"},
		{name: "single case insensitive", value: "{client_header:openai-project}", want: "project-a"},
		{name: "mixed", value: "tenant-{client_header:OpenAI-Project}", want: "tenant-project-a"},
		{name: "multiple", value: "{client_header:OpenAI-Organization}/{client_header:OpenAI-Project}", want: "org-a/project-a"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := RenderClientHeaderTemplate(test.value, source)
			if !result.Apply {
				t.Fatalf("render unexpectedly skipped: %s", result.Reason)
			}
			if result.Value != test.want {
				t.Fatalf("render=%q, want %q", result.Value, test.want)
			}
		})
	}
}

func TestRenderClientHeaderTemplateMissingSourceSkipsHeader(t *testing.T) {
	result := RenderClientHeaderTemplate("tenant-{client_header:X-Tenant-ID}", nil)
	if result.Apply {
		t.Fatalf("missing source unexpectedly rendered %q", result.Value)
	}
	if result.Reason != "missing_source" {
		t.Fatalf("reason=%q, want missing_source", result.Reason)
	}
}

func TestRenderClientHeaderTemplateDoesNotRecurse(t *testing.T) {
	source := http.Header{"X-Tenant-Id": []string{"{client_header:Authorization}"}}
	result := RenderClientHeaderTemplate("{client_header:X-Tenant-ID}", source)
	if !result.Apply {
		t.Fatalf("render unexpectedly skipped: %s", result.Reason)
	}
	if result.Value != "{client_header:Authorization}" {
		t.Fatalf("render recursively expanded source: %q", result.Value)
	}
}

func TestValidateClientHeaderTemplateRejectsUnsafeOrMalformedSources(t *testing.T) {
	invalid := []string{
		"{client_header:Authorization}",
		"{client_header:X-Api-Key}",
		"{client_header:Cookie}",
		"{client_header:X-Forwarded-For}",
		"{client_header:Sec-WebSocket-Key}",
		"{client_header:}",
		"{client_header:OpenAI-Project",
	}
	for _, value := range invalid {
		if err := ValidateClientHeaderTemplate(value); err == nil {
			t.Fatalf("ValidateClientHeaderTemplate(%q) unexpectedly succeeded", value)
		}
	}
}

func TestSnapshotClientHeaderTemplateSourceKeepsOnlySafeMetadata(t *testing.T) {
	source := http.Header{
		"Openai-Project":    []string{"project-a"},
		"X-Tenant-Id":      []string{"tenant-a"},
		"Authorization":    []string{"Bearer secret"},
		"Cookie":           []string{"session=secret"},
		"X-Forwarded-For":  []string{"203.0.113.1"},
		"Sec-Websocket-Key": []string{"ws-secret"},
		"Origin":           []string{"https://client.example"},
	}

	snapshot := SnapshotClientHeaderTemplateSource(source)
	if got := snapshot.Get("OpenAI-Project"); got != "project-a" {
		t.Fatalf("OpenAI-Project=%q, want project-a", got)
	}
	if got := snapshot.Get("X-Tenant-ID"); got != "tenant-a" {
		t.Fatalf("X-Tenant-ID=%q, want tenant-a", got)
	}
	for _, blocked := range []string{"Authorization", "Cookie", "X-Forwarded-For", "Sec-WebSocket-Key", "Origin"} {
		if got := snapshot.Get(blocked); got != "" {
			t.Fatalf("blocked source %s retained value %q", blocked, got)
		}
	}
}

func TestSnapshotClientHeaderTemplateSourceRejectsInvalidFieldValue(t *testing.T) {
	source := http.Header{"X-Tenant-Id": []string{"good\r\nbad"}}
	if snapshot := SnapshotClientHeaderTemplateSource(source); snapshot != nil {
		t.Fatalf("invalid header value retained in snapshot: %#v", snapshot)
	}
}

func TestClientHeaderTemplateTargetProtection(t *testing.T) {
	for _, blocked := range []string{"Authorization", "X-Api-Key", "Host", "Sec-WebSocket-Protocol"} {
		if IsAllowedClientHeaderTemplateTarget(blocked) {
			t.Fatalf("protected target %q unexpectedly allowed", blocked)
		}
	}
	if !IsAllowedClientHeaderTemplateTarget("X-Upstream-Project") {
		t.Fatal("safe custom-header target unexpectedly blocked")
	}
}
