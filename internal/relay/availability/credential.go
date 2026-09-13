package availability

import (
	"sync"
	"time"
)

type credentialKey struct {
	channelID int
	keyID     int
	revision  int
}

type credentialEntry struct {
	cooldownUntil time.Time
	failureStreak int
	lastFailureAt time.Time
	lastSuccessAt time.Time
	reason        string
}

var credentialRuntime = struct {
	mu      sync.Mutex
	entries map[credentialKey]*credentialEntry
}{entries: make(map[credentialKey]*credentialEntry)}

func normalizeCredentialRevision(revision int) int {
	if revision <= 0 {
		return 1
	}
	return revision
}

func credentialCooldown(streak int) time.Duration {
	switch streak {
	case 1:
		return 5 * time.Minute
	case 2:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}

func transientCredentialCooldown(streak int) time.Duration {
	switch streak {
	case 1:
		return 15 * time.Second
	case 2:
		return 60 * time.Second
	default:
		return 5 * time.Minute
	}
}

// pruneOlderCredentialRevisionsLocked removes only superseded identities.
// Never delete a newer revision: an in-flight request created before a secret
// replacement may finish later and must not erase runtime state for the new key.
func pruneOlderCredentialRevisionsLocked(channelID, keyID, revision int) {
	for key := range credentialRuntime.entries {
		if key.channelID == channelID && key.keyID == keyID && key.revision < revision {
			delete(credentialRuntime.entries, key)
		}
	}
}

// CredentialAvailableRevision returns whether one concrete credential identity
// may participate in scheduling. CredentialRevision is part of the runtime
// identity so replacing a secret under the same key ID cannot inherit stale
// auth/quota cooldown from the previous credential.
func CredentialAvailableRevision(channelID, keyID, revision int, now time.Time) bool {
	if keyID <= 0 {
		return false
	}
	revision = normalizeCredentialRevision(revision)
	credentialRuntime.mu.Lock()
	defer credentialRuntime.mu.Unlock()
	pruneOlderCredentialRevisionsLocked(channelID, keyID, revision)
	e := credentialRuntime.entries[credentialKey{channelID: channelID, keyID: keyID, revision: revision}]
	return e == nil || !e.cooldownUntil.After(now)
}

// CredentialAvailable is kept for legacy callers and tests whose credentials
// predate explicit identity generation.
func CredentialAvailable(channelID, keyID int, now time.Time) bool {
	return CredentialAvailableRevision(channelID, keyID, 1, now)
}

func recordCredentialFailure(channelID, keyID, revision int, reason string, now time.Time, cooldown func(int) time.Duration) time.Time {
	if keyID <= 0 {
		return time.Time{}
	}
	revision = normalizeCredentialRevision(revision)
	credentialRuntime.mu.Lock()
	defer credentialRuntime.mu.Unlock()
	pruneOlderCredentialRevisionsLocked(channelID, keyID, revision)
	key := credentialKey{channelID: channelID, keyID: keyID, revision: revision}
	e := credentialRuntime.entries[key]
	if e == nil {
		e = &credentialEntry{}
		credentialRuntime.entries[key] = e
	}
	e.failureStreak++
	e.reason = reason
	e.lastFailureAt = now
	e.cooldownUntil = now.Add(cooldown(e.failureStreak))
	return e.cooldownUntil
}

func RecordCredentialFailureRevision(channelID, keyID, revision int, reason string, now time.Time) time.Time {
	return recordCredentialFailure(channelID, keyID, revision, reason, now, credentialCooldown)
}

func RecordCredentialTransientFailureRevision(channelID, keyID, revision int, reason string, now time.Time) time.Time {
	return recordCredentialFailure(channelID, keyID, revision, reason, now, transientCredentialCooldown)
}

func RecordCredentialFailure(channelID, keyID int, reason string, now time.Time) time.Time {
	return RecordCredentialFailureRevision(channelID, keyID, 1, reason, now)
}

func RecordCredentialTransientFailure(channelID, keyID int, reason string, now time.Time) time.Time {
	return RecordCredentialTransientFailureRevision(channelID, keyID, 1, reason, now)
}

func RecordCredentialSuccessRevision(channelID, keyID, revision int, now time.Time) {
	if keyID <= 0 {
		return
	}
	revision = normalizeCredentialRevision(revision)
	credentialRuntime.mu.Lock()
	defer credentialRuntime.mu.Unlock()
	pruneOlderCredentialRevisionsLocked(channelID, keyID, revision)
	key := credentialKey{channelID: channelID, keyID: keyID, revision: revision}
	e := credentialRuntime.entries[key]
	if e == nil {
		return
	}
	e.failureStreak = 0
	e.reason = ""
	e.cooldownUntil = time.Time{}
	e.lastSuccessAt = now
}

func RecordCredentialSuccess(channelID, keyID int, now time.Time) {
	RecordCredentialSuccessRevision(channelID, keyID, 1, now)
}

func resetCredentialRuntime() {
	credentialRuntime.mu.Lock()
	credentialRuntime.entries = make(map[credentialKey]*credentialEntry)
	credentialRuntime.mu.Unlock()
	resetCredentialFairness()
	resetCapabilityRuntime()
}
