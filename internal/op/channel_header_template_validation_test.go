package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestChannelCreateRejectsBlockedClientHeaderTemplate(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	channel := newHeaderTemplateValidationChannel("blocked-template-create")
	channel.CustomHeader = []model.CustomHeader{{
		HeaderKey:   "X-Leak",
		HeaderValue: "{client_header:Authorization}",
	}}

	if err := ChannelCreate(channel, ctx); err == nil {
		t.Fatal("ChannelCreate accepted blocked Authorization template")
	}
}

func TestChannelCreateRejectsMalformedClientHeaderTemplate(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	channel := newHeaderTemplateValidationChannel("malformed-template-create")
	channel.CustomHeader = []model.CustomHeader{{
		HeaderKey:   "X-Upstream-Project",
		HeaderValue: "tenant-{client_header:OpenAI-Project",
	}}

	if err := ChannelCreate(channel, ctx); err == nil {
		t.Fatal("ChannelCreate accepted malformed client_header template")
	}
}

func TestChannelUpdateRejectsBlockedClientHeaderTemplate(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	channel := newHeaderTemplateValidationChannel("blocked-template-update")
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate setup failed: %v", err)
	}
	invalid := []model.CustomHeader{{
		HeaderKey:   "X-Leak",
		HeaderValue: "{client_header:X-Api-Key}",
	}}

	if _, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: channel.ID, CustomHeader: &invalid}, ctx); err == nil {
		t.Fatal("ChannelUpdate accepted blocked X-Api-Key template")
	}
}

func TestChannelCreateAllowsSafeClientHeaderTemplate(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	channel := newHeaderTemplateValidationChannel("safe-template-create")
	channel.CustomHeader = []model.CustomHeader{{
		HeaderKey:   "X-Upstream-Project",
		HeaderValue: "tenant-{client_header:OpenAI-Project}",
	}}

	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate rejected safe client_header template: %v", err)
	}
}

func newHeaderTemplateValidationChannel(name string) *model.Channel {
	return &model.Channel{
		Name:     name,
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://example.test/v1"}},
		Model:    "template-model",
		Keys: []model.ChannelKey{{
			Enabled: true, ChannelKey: "test-secret",
		}},
	}
}
