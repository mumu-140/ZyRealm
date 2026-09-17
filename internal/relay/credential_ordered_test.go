package relay

import (
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
)

func TestSelectOrderedAvailableCredentialSkipsCoolingLowestCostKey(t *testing.T) {
	availability.Reset()
	t.Cleanup(availability.Reset)

	now := time.Now()
	channel := &dbmodel.Channel{
		ID: 2101,
		Keys: []dbmodel.ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "cheap", TotalCost: 0, CredentialRevision: 1},
			{ID: 2, Enabled: true, ChannelKey: "fallback", TotalCost: 10, CredentialRevision: 1},
		},
	}
	availability.RecordCredentialFailureRevision(channel.ID, 1, 1, "invalid_api_key", now)

	excluded := make(map[int]struct{})
	got := selectOrderedAvailableCredential(channel, dbmodel.ChannelKeySelectOptions{ExcludeKeyIDs: excluded}, nil, now)
	if got.ID != 2 {
		t.Fatalf("selected key = %d, want 2", got.ID)
	}
	if _, ok := excluded[1]; !ok {
		t.Fatal("cooling key must be added to caller exclusion set")
	}
}

func TestSelectOrderedAvailableCredentialPreservesHealthyPreferredKey(t *testing.T) {
	availability.Reset()
	t.Cleanup(availability.Reset)

	now := time.Now()
	channel := &dbmodel.Channel{
		ID: 2102,
		Keys: []dbmodel.ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "cheap", TotalCost: 0, CredentialRevision: 1},
			{ID: 2, Enabled: true, ChannelKey: "preferred", TotalCost: 10, CredentialRevision: 1},
		},
	}

	got := selectOrderedAvailableCredential(channel, dbmodel.ChannelKeySelectOptions{
		ExcludeKeyIDs:  make(map[int]struct{}),
		PreferredKeyID: 2,
	}, nil, now)
	if got.ID != 2 {
		t.Fatalf("selected key = %d, want preferred key 2", got.ID)
	}
}
