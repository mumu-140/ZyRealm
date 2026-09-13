package availability

import (
	"testing"
	"time"
)

func TestCredentialRevisionDoesNotInheritCooldown(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	now := time.Date(2026, time.September, 13, 10, 0, 0, 0, time.UTC)

	RecordCredentialFailureRevision(7, 11, 1, "old-secret-invalid", now)
	if CredentialAvailableRevision(7, 11, 1, now.Add(time.Second)) {
		t.Fatalf("revision 1 should be cooling after auth failure")
	}
	if !CredentialAvailableRevision(7, 11, 2, now.Add(time.Second)) {
		t.Fatalf("replacement credential revision must bypass old cooldown")
	}
}

func TestOldCredentialResultCannotEraseNewRevisionState(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	now := time.Date(2026, time.September, 13, 10, 0, 0, 0, time.UTC)

	RecordCredentialFailureRevision(8, 12, 2, "new-secret-invalid", now)
	RecordCredentialSuccessRevision(8, 12, 1, now.Add(time.Second))
	if CredentialAvailableRevision(8, 12, 2, now.Add(2*time.Second)) {
		t.Fatalf("late success from old revision must not clear new revision cooldown")
	}

	RecordCredentialFailureRevision(8, 12, 1, "late-old-failure", now.Add(3*time.Second))
	if CredentialAvailableRevision(8, 12, 2, now.Add(4*time.Second)) {
		t.Fatalf("late failure from old revision must not overwrite new revision cooldown")
	}
}
