package relay

import (
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
)

func credentialDecisionTestIterator(channelID int) *balancer.Iterator {
	return balancer.NewIterator(dbmodel.Group{
		Mode: dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{{
			ChannelID: channelID,
			ModelName: "upstream-model",
			Priority:  1,
		}},
	}, 0, "request-model")
}

func credentialDecisionTestChannel() *dbmodel.Channel {
	return &dbmodel.Channel{
		ID:      701,
		Name:    "credential-provider",
		Enabled: true,
		Keys: []dbmodel.ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "key-1", CredentialRevision: 1},
			{ID: 2, Enabled: true, ChannelKey: "key-2", CredentialRevision: 1},
			{ID: 3, Enabled: true, ChannelKey: "key-3", CredentialRevision: 1},
		},
	}
}

func findCredentialDecision(attempts []dbmodel.ChannelAttempt, keyID int) (dbmodel.RoutingDecisionEvent, bool) {
	for _, attempt := range attempts {
		for _, event := range attempt.DecisionEvents {
			if event.Stage == dbmodel.DecisionStageCredential && event.ChannelKeyID == keyID {
				return event, true
			}
		}
	}
	return dbmodel.RoutingDecisionEvent{}, false
}

func TestSelectFairChannelCredentialTracesCooldownWithoutChangingSelection(t *testing.T) {
	availability.Reset()
	t.Cleanup(availability.Reset)
	now := time.Now()
	channel := credentialDecisionTestChannel()
	availability.RecordCredentialFailureRevision(channel.ID, 1, 1, "auth_failure", now)

	iterator := credentialDecisionTestIterator(channel.ID)
	if !iterator.Next() {
		t.Fatal("expected provider candidate")
	}
	selected := selectFairChannelCredential(channel, dbmodel.ChannelKeySelectOptions{}, iterator, now)
	if selected.ID != 2 {
		t.Fatalf("selected key = %d, want 2; tracing must not change fair survivor selection", selected.ID)
	}

	event, ok := findCredentialDecision(iterator.Attempts(), 1)
	if !ok {
		t.Fatal("missing credential_cooldown decision event")
	}
	if event.Outcome != dbmodel.DecisionOutcomeRejected || event.Reason != dbmodel.DecisionReasonCredentialCooldown {
		t.Fatalf("credential event = outcome %q reason %q", event.Outcome, event.Reason)
	}
	if event.ChannelID != channel.ID || event.ChannelName != channel.Name {
		t.Fatalf("credential event identity = channel %d name %q", event.ChannelID, event.ChannelName)
	}
}

func TestSelectFairChannelCredentialDoesNotTraceAlreadyExcludedKey(t *testing.T) {
	availability.Reset()
	t.Cleanup(availability.Reset)
	now := time.Now()
	channel := credentialDecisionTestChannel()
	availability.RecordCredentialFailureRevision(channel.ID, 1, 1, "auth_failure", now)

	iterator := credentialDecisionTestIterator(channel.ID)
	if !iterator.Next() {
		t.Fatal("expected provider candidate")
	}
	selected := selectFairChannelCredential(channel, dbmodel.ChannelKeySelectOptions{
		ExcludeKeyIDs: map[int]struct{}{1: {}},
	}, iterator, now)
	if selected.ID != 2 {
		t.Fatalf("selected key = %d, want 2", selected.ID)
	}
	if _, ok := findCredentialDecision(iterator.Attempts(), 1); ok {
		t.Fatal("already request-excluded key must not be misreported as a fresh credential cooldown rejection")
	}
}
