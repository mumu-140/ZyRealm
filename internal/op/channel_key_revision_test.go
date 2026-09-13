package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestChannelUpdateAdvancesCredentialRevisionOnlyForSecretReplacement(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	channel := &model.Channel{
		Name:     "credential-revision-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://example.test/v1"}},
		Model:    "revision-model",
		Keys: []model.ChannelKey{{
			Enabled: true, ChannelKey: "old-secret", Remark: "old-remark",
		}},
	}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	if len(channel.Keys) != 1 {
		t.Fatalf("expected one key, got %d", len(channel.Keys))
	}
	keyID := channel.Keys[0].ID

	enabled := false
	updated, err := ChannelUpdate(&model.ChannelUpdateRequest{
		ID: channel.ID,
		KeysToUpdate: []model.ChannelKeyUpdateRequest{{ID: keyID, Enabled: &enabled}},
	}, ctx)
	if err != nil {
		t.Fatalf("enabled-only ChannelUpdate failed: %v", err)
	}
	if got := credentialRevisionForTest(t, updated, keyID); got != 1 {
		t.Fatalf("enabled-only update revision=%d, want 1", got)
	}

	newSecret := "new-secret"
	updated, err = ChannelUpdate(&model.ChannelUpdateRequest{
		ID: channel.ID,
		KeysToUpdate: []model.ChannelKeyUpdateRequest{{ID: keyID, ChannelKey: &newSecret}},
	}, ctx)
	if err != nil {
		t.Fatalf("secret ChannelUpdate failed: %v", err)
	}
	if got := credentialRevisionForTest(t, updated, keyID); got != 2 {
		t.Fatalf("secret replacement revision=%d, want 2", got)
	}

	remark := "new-remark"
	updated, err = ChannelUpdate(&model.ChannelUpdateRequest{
		ID: channel.ID,
		KeysToUpdate: []model.ChannelKeyUpdateRequest{{ID: keyID, Remark: &remark}},
	}, ctx)
	if err != nil {
		t.Fatalf("remark-only ChannelUpdate failed: %v", err)
	}
	if got := credentialRevisionForTest(t, updated, keyID); got != 2 {
		t.Fatalf("remark-only update revision=%d, want 2", got)
	}
}

func credentialRevisionForTest(t *testing.T, channel *model.Channel, keyID int) int {
	t.Helper()
	if channel == nil {
		t.Fatalf("channel is nil")
	}
	for _, key := range channel.Keys {
		if key.ID == keyID {
			if key.CredentialRevision <= 0 {
				return 1
			}
			return key.CredentialRevision
		}
	}
	t.Fatalf("key %d not found", keyID)
	return 0
}
