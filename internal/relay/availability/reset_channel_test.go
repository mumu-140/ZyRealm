package availability

import (
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
)

func TestResetChannelClearsOnlyTargetProviderState(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	for _, channelID := range []int{71, 72} {
		RecordProviderFailure(channelID, "provider failure", now)
		RecordModelFailure(channelID, "model-a", "model failure", now)
		RecordCredentialFailureRevision(channelID, channelID*10, 2, "credential failure", now)
		RecordCapabilityNegative(channelID, "model-a", "sig", "cfg", "unsupported", now)
		SelectCredentialFair(channelID, []dbmodel.ChannelKey{{
			ID: channelID * 10, ChannelID: channelID, Enabled: true,
			ChannelKey: "key", CredentialRevision: 2,
		}}, 0)
	}

	ResetChannel(71)

	if got := CandidateState(71, "model-a", now); got != StateAvailable {
		t.Fatalf("target candidate state=%v, want available", got)
	}
	if !CredentialAvailableRevision(71, 710, 2, now) {
		t.Fatalf("target credential cooldown survived channel reset")
	}
	if got := CapabilityInfo(71, "model-a", "sig", "cfg", now); got.Blocked {
		t.Fatalf("target capability negative survived channel reset: %+v", got)
	}
	credentialFairRuntime.mu.Lock()
	_, targetLedgerExists := credentialFairRuntime.ledgers[71]
	_, otherLedgerExists := credentialFairRuntime.ledgers[72]
	credentialFairRuntime.mu.Unlock()
	if targetLedgerExists {
		t.Fatalf("target fairness ledger survived channel reset")
	}

	if got := CandidateState(72, "model-a", now); got != StateCooldown {
		t.Fatalf("other candidate state=%v, want cooldown", got)
	}
	if CredentialAvailableRevision(72, 720, 2, now) {
		t.Fatalf("other credential cooldown was cleared by target reset")
	}
	if got := CapabilityInfo(72, "model-a", "sig", "cfg", now); !got.Blocked {
		t.Fatalf("other capability negative was cleared by target reset")
	}
	if !otherLedgerExists {
		t.Fatalf("other fairness ledger was cleared by target reset")
	}
}
