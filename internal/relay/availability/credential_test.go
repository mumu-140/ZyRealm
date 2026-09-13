package availability

import (
	"testing"
	"time"
)

func TestCredentialCooldownAndRecovery(t *testing.T) {
	Reset()
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	if !CredentialAvailable(10, 101, base) {
		t.Fatalf("fresh credential should be available")
	}
	until := RecordCredentialFailure(10, 101, "invalid_credential", base)
	if want := base.Add(5 * time.Minute); !until.Equal(want) {
		t.Fatalf("first credential cooldown = %v, want %v", until, want)
	}
	if CredentialAvailable(10, 101, base.Add(time.Minute)) {
		t.Fatalf("credential should remain cooling before deadline")
	}
	if !CredentialAvailable(10, 101, until) {
		t.Fatalf("credential should re-enter after cooldown deadline")
	}
}

func TestCredentialFailureDoesNotAffectSiblingKey(t *testing.T) {
	Reset()
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	RecordCredentialFailure(20, 201, "insufficient_balance", base)
	if CredentialAvailable(20, 201, base) {
		t.Fatalf("failed key should be cooling")
	}
	if !CredentialAvailable(20, 202, base) {
		t.Fatalf("sibling key should remain available")
	}
}

func TestCredentialSuccessClearsFailureStreak(t *testing.T) {
	Reset()
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	RecordCredentialFailure(30, 301, "invalid_credential", base)
	RecordCredentialSuccess(30, 301, base.Add(time.Minute))
	if !CredentialAvailable(30, 301, base.Add(time.Minute)) {
		t.Fatalf("credential success should clear cooldown")
	}
	until := RecordCredentialFailure(30, 301, "invalid_credential", base.Add(2*time.Minute))
	if want := base.Add(7 * time.Minute); !until.Equal(want) {
		t.Fatalf("post-success failure should restart at first cooldown stage: got %v want %v", until, want)
	}
}
