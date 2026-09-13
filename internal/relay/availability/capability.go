package availability

import (
	"strings"
	"sync"
	"time"
)

const (
	capabilityNegativeTTL = 30 * time.Minute
	capabilityGCThreshold = 1024
	// Capability signatures are request-shaped and can be high-cardinality.
	// Keep the cache bounded even when every active entry is still inside TTL.
	// Eviction is safe: losing negative evidence can only cause another real
	// capability attempt; it cannot incorrectly exclude a healthy candidate.
	capabilityMaxEntries = 4096
)

type capabilityKey struct {
	channelID         int
	model             string
	signature         string
	configFingerprint string
}

type capabilityEntry struct {
	reason    string
	expiresAt time.Time
}

type CapabilitySnapshot struct {
	Blocked   bool
	Reason    string
	ExpiresAt time.Time
}

type capabilityRegistry struct {
	mu      sync.Mutex
	entries map[capabilityKey]capabilityEntry
}

var capabilityShared = capabilityRegistry{entries: make(map[capabilityKey]capabilityEntry)}

func capabilityCacheKey(channelID int, model, signature, configFingerprint string) capabilityKey {
	return capabilityKey{
		channelID:         channelID,
		model:             model,
		signature:         signature,
		configFingerprint: configFingerprint,
	}
}

// CapabilityInfo reports whether an exact provider-model capability shape is
// temporarily ineligible. Configuration fingerprinting versions the evidence:
// changing a capability-affecting channel setting immediately bypasses stale
// observations without mutating provider health.
func CapabilityInfo(channelID int, model, signature, configFingerprint string, now time.Time) CapabilitySnapshot {
	if channelID <= 0 || strings.TrimSpace(model) == "" || strings.TrimSpace(signature) == "" || strings.TrimSpace(configFingerprint) == "" {
		return CapabilitySnapshot{}
	}
	capabilityShared.mu.Lock()
	defer capabilityShared.mu.Unlock()

	key := capabilityCacheKey(channelID, model, signature, configFingerprint)
	entry, ok := capabilityShared.entries[key]
	if !ok {
		return CapabilitySnapshot{}
	}
	if !entry.expiresAt.After(now) {
		delete(capabilityShared.entries, key)
		return CapabilitySnapshot{}
	}
	return CapabilitySnapshot{Blocked: true, Reason: entry.reason, ExpiresAt: entry.expiresAt}
}

func trimCapabilityEntriesLocked(now time.Time) {
	if len(capabilityShared.entries) >= capabilityGCThreshold {
		for key, entry := range capabilityShared.entries {
			if !entry.expiresAt.After(now) {
				delete(capabilityShared.entries, key)
			}
		}
	}
	for len(capabilityShared.entries) >= capabilityMaxEntries {
		var oldestKey capabilityKey
		var oldestExpiry time.Time
		found := false
		for key, entry := range capabilityShared.entries {
			if !found || entry.expiresAt.Before(oldestExpiry) {
				oldestKey = key
				oldestExpiry = entry.expiresAt
				found = true
			}
		}
		if !found {
			break
		}
		delete(capabilityShared.entries, oldestKey)
	}
}

// RecordCapabilityNegative temporarily excludes only the exact capability
// shape. This is eligibility memory, not provider health, so it has no streak,
// half-open state, or circuit effect.
func RecordCapabilityNegative(channelID int, model, signature, configFingerprint, reason string, now time.Time) time.Time {
	if channelID <= 0 || strings.TrimSpace(model) == "" || strings.TrimSpace(signature) == "" || strings.TrimSpace(configFingerprint) == "" {
		return time.Time{}
	}
	expiresAt := now.Add(capabilityNegativeTTL)
	key := capabilityCacheKey(channelID, model, signature, configFingerprint)
	capabilityShared.mu.Lock()
	defer capabilityShared.mu.Unlock()
	if _, exists := capabilityShared.entries[key]; !exists {
		trimCapabilityEntriesLocked(now)
	}
	capabilityShared.entries[key] = capabilityEntry{
		reason:    reason,
		expiresAt: expiresAt,
	}
	return expiresAt
}

// ClearCapabilityNegative lets concurrent successful real traffic invalidate an
// exact negative observation early. The configuration fingerprint prevents a
// late success from an older channel configuration from clearing newer evidence.
func ClearCapabilityNegative(channelID int, model, signature, configFingerprint string) {
	if channelID <= 0 || strings.TrimSpace(model) == "" || strings.TrimSpace(signature) == "" || strings.TrimSpace(configFingerprint) == "" {
		return
	}
	capabilityShared.mu.Lock()
	delete(capabilityShared.entries, capabilityCacheKey(channelID, model, signature, configFingerprint))
	capabilityShared.mu.Unlock()
}

func resetCapabilityRuntime() {
	capabilityShared.mu.Lock()
	capabilityShared.entries = make(map[capabilityKey]capabilityEntry)
	capabilityShared.mu.Unlock()
}
