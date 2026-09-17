package relay

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
)

func TestRoutingDecisionGeneric500StaysRuntimeNeutralDuringLegacyCircuitCompatibility(t *testing.T) {
	result := attemptResult{
		Err:        errors.New("channel failed"),
		StatusCode: http.StatusInternalServerError,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if decision.Domain != failureDomainUnknown {
		t.Fatalf("domain = %v, want unknown", decision.Domain)
	}
	if decision.RuntimeEffect != routingRuntimeNone {
		t.Fatalf("runtime effect = %q, want none", decision.RuntimeEffect)
	}
	if decision.OutlierScope != scopeChannel {
		t.Fatalf("outlier scope = %v, want channel statistical evidence", decision.OutlierScope)
	}
	// P4A deliberately preserves the legacy breaker write for revision-1
	// compatibility. The circuit stops being an authority in later P4 gates,
	// after all shared consumers (notably Images) have migrated.
	if decision.CircuitEffect != "record_failure" {
		t.Fatalf("circuit effect = %q, want temporary compatibility write", decision.CircuitEffect)
	}
}

func TestCoreCredentialSelectionNewRevisionDoesNotInheritLegacyCircuit(t *testing.T) {
	balancer.Reset()
	t.Cleanup(balancer.Reset)
	now := time.Now()
	channel := &dbmodel.Channel{
		ID:      1701,
		Name:    "p4a-core-credential",
		Enabled: true,
		Keys: []dbmodel.ChannelKey{
			{ID: 11, Enabled: true, ChannelKey: "key-11", CredentialRevision: 7},
		},
	}
	iterator := balancer.NewIterator(dbmodel.Group{
		Mode: dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{{
			ChannelID: channel.ID,
			ModelName: "upstream-model",
			Priority:  1,
		}},
	}, 0, "request-model")
	if !iterator.Next() {
		t.Fatal("expected provider candidate")
	}

	for i := 0; i < 5; i++ {
		balancer.RecordFailure(channel.ID, 11, "upstream-model", balancer.FailureHard)
	}
	if tripped, _ := balancer.IsTripped(channel.ID, 11, "upstream-model"); !tripped {
		t.Fatal("test precondition: legacy circuit must be open")
	}
	if !availability.CredentialAvailableRevision(channel.ID, 11, 7, now) {
		t.Fatal("test precondition: new credential revision must remain healthy")
	}

	selected := selectFairChannelCredential(channel, dbmodel.ChannelKeySelectOptions{}, iterator, now)
	if selected.ID != 11 {
		t.Fatalf("selected key = %d, want 11; a new credential revision must not inherit stale legacy circuit state", selected.ID)
	}
}

func TestCoreCredentialSelectionRevisionOneKeepsLegacyCircuitCompatibility(t *testing.T) {
	balancer.Reset()
	t.Cleanup(balancer.Reset)
	now := time.Now()
	channel := &dbmodel.Channel{
		ID:      1702,
		Name:    "p4a-core-legacy-credential",
		Enabled: true,
		Keys: []dbmodel.ChannelKey{
			{ID: 12, Enabled: true, ChannelKey: "key-12", CredentialRevision: 1},
		},
	}
	iterator := balancer.NewIterator(dbmodel.Group{
		Mode: dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{{
			ChannelID: channel.ID,
			ModelName: "upstream-model",
			Priority:  1,
		}},
	}, 0, "request-model")
	if !iterator.Next() {
		t.Fatal("expected provider candidate")
	}

	for i := 0; i < 5; i++ {
		balancer.RecordFailure(channel.ID, 12, "upstream-model", balancer.FailureHard)
	}
	selected := selectFairChannelCredential(channel, dbmodel.ChannelKeySelectOptions{}, iterator, now)
	if selected.ID != 0 {
		t.Fatalf("selected key = %d, want none while revision-1 compatibility circuit is open", selected.ID)
	}
}
