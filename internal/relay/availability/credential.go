package availability

import (
	"sync"
	"time"
)

type credentialKey struct {
	channelID int
	keyID     int
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

// CredentialAvailable returns whether the key may participate in scheduling.
// Credential cooldown uses time-based re-entry instead of a separate half-open
// lease; provider/model scopes own the more expensive passive single-flight
// recovery state machine.
func CredentialAvailable(channelID, keyID int, now time.Time) bool {
	if keyID <= 0 {
		return false
	}
	credentialRuntime.mu.Lock()
	defer credentialRuntime.mu.Unlock()
	e := credentialRuntime.entries[credentialKey{channelID: channelID, keyID: keyID}]
	return e == nil || !e.cooldownUntil.After(now)
}

func recordCredentialFailure(channelID, keyID int, reason string, now time.Time, cooldown func(int) time.Duration) time.Time {
	if keyID <= 0 {
		return time.Time{}
	}
	credentialRuntime.mu.Lock()
	defer credentialRuntime.mu.Unlock()
	key := credentialKey{channelID: channelID, keyID: keyID}
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

func RecordCredentialFailure(channelID, keyID int, reason string, now time.Time) time.Time {
	return recordCredentialFailure(channelID, keyID, reason, now, credentialCooldown)
}

func RecordCredentialTransientFailure(channelID, keyID int, reason string, now time.Time) time.Time {
	return recordCredentialFailure(channelID, keyID, reason, now, transientCredentialCooldown)
}

func RecordCredentialSuccess(channelID, keyID int, now time.Time) {
	if keyID <= 0 {
		return
	}
	credentialRuntime.mu.Lock()
	defer credentialRuntime.mu.Unlock()
	key := credentialKey{channelID: channelID, keyID: keyID}
	e := credentialRuntime.entries[key]
	if e == nil {
		return
	}
	e.failureStreak = 0
	e.reason = ""
	e.cooldownUntil = time.Time{}
	e.lastSuccessAt = now
}

func resetCredentialRuntime() {
	credentialRuntime.mu.Lock()
	credentialRuntime.entries = make(map[credentialKey]*credentialEntry)
	credentialRuntime.mu.Unlock()
}
