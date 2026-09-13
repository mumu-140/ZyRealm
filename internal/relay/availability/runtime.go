package availability

import (
	"sync"
	"time"
)

type State uint8

const (
	StateAvailable State = iota
	StateSuspect
	StateCooldown
	StateHalfOpen
)

const suspectEscalationWindow = 30 * time.Second

type scope uint8

const (
	scopeProvider scope = iota
	scopeProviderModel
)

type runtimeKey struct {
	scope     scope
	channelID int
	model     string
}

type entry struct {
	state            State
	reason           string
	since            time.Time
	cooldownUntil    time.Time
	failureStreak    int
	successStreak    int
	lastFailureAt    time.Time
	lastSuccessAt    time.Time
	halfOpenInFlight bool
}

type Lease struct {
	keys []runtimeKey
}

type CandidateSnapshot struct {
	State         State
	Reason        string
	CooldownUntil time.Time
}

type registry struct {
	mu      sync.Mutex
	entries map[runtimeKey]*entry
}

var shared = registry{entries: make(map[runtimeKey]*entry)}

func providerKey(channelID int) runtimeKey {
	return runtimeKey{scope: scopeProvider, channelID: channelID}
}

func modelKey(channelID int, model string) runtimeKey {
	return runtimeKey{scope: scopeProviderModel, channelID: channelID, model: model}
}

func stateAt(e *entry, now time.Time) State {
	if e == nil {
		return StateAvailable
	}
	switch e.state {
	case StateCooldown:
		if e.cooldownUntil.After(now) || e.halfOpenInFlight {
			return StateCooldown
		}
		return StateHalfOpen
	case StateHalfOpen:
		if e.halfOpenInFlight {
			return StateCooldown
		}
		return StateHalfOpen
	case StateSuspect:
		return StateSuspect
	default:
		return StateAvailable
	}
}

func combineState(provider, model State) State {
	if provider == StateCooldown || model == StateCooldown {
		return StateCooldown
	}
	if provider == StateHalfOpen || model == StateHalfOpen {
		return StateHalfOpen
	}
	if provider == StateSuspect || model == StateSuspect {
		return StateSuspect
	}
	return StateAvailable
}

func CandidateState(channelID int, model string, now time.Time) State {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	return combineState(
		stateAt(shared.entries[providerKey(channelID)], now),
		stateAt(shared.entries[modelKey(channelID, model)], now),
	)
}

func CandidateInfo(channelID int, model string, now time.Time) CandidateSnapshot {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	provider := shared.entries[providerKey(channelID)]
	modelEntry := shared.entries[modelKey(channelID, model)]
	state := combineState(stateAt(provider, now), stateAt(modelEntry, now))
	out := CandidateSnapshot{State: state}
	for _, e := range []*entry{provider, modelEntry} {
		if e == nil {
			continue
		}
		if e.reason != "" && out.Reason == "" {
			out.Reason = e.reason
		}
		if e.cooldownUntil.After(out.CooldownUntil) {
			out.CooldownUntil = e.cooldownUntil
		}
	}
	return out
}

// AcquireCandidate atomically acquires any expired cooldowns that require a
// passive half-open trial. Available and suspect candidates need no lease.
func AcquireCandidate(channelID int, model string, now time.Time) (Lease, bool) {
	shared.mu.Lock()
	defer shared.mu.Unlock()

	keys := []runtimeKey{providerKey(channelID), modelKey(channelID, model)}
	for _, key := range keys {
		if stateAt(shared.entries[key], now) == StateCooldown {
			return Lease{}, false
		}
	}

	lease := Lease{}
	for _, key := range keys {
		e := shared.entries[key]
		if stateAt(e, now) != StateHalfOpen {
			continue
		}
		e.state = StateHalfOpen
		e.halfOpenInFlight = true
		lease.keys = append(lease.keys, key)
	}
	return lease, true
}

// ReleaseLease releases an unused or semantically neutral half-open trial. A
// caller that recorded success/failure first has already transitioned the entry,
// so this becomes a no-op for that key. Neutral outcomes return to an unleased
// HALF_OPEN state, preserving single-flight recovery until a real health signal
// is observed.
func ReleaseLease(lease Lease, now time.Time) {
	if len(lease.keys) == 0 {
		return
	}
	shared.mu.Lock()
	defer shared.mu.Unlock()
	for _, key := range lease.keys {
		e := shared.entries[key]
		if e == nil || e.state != StateHalfOpen || !e.halfOpenInFlight {
			continue
		}
		e.state = StateHalfOpen
		e.since = now
		e.halfOpenInFlight = false
	}
}

func providerCooldown(streak int) time.Duration {
	switch streak {
	case 1:
		return 5 * time.Second
	case 2:
		return 15 * time.Second
	case 3:
		return 60 * time.Second
	default:
		return 5 * time.Minute
	}
}

func modelCooldown(streak int) time.Duration {
	switch streak {
	case 1:
		return 15 * time.Second
	case 2:
		return 60 * time.Second
	default:
		return 5 * time.Minute
	}
}

func recordCooldown(key runtimeKey, reason string, now time.Time, duration func(int) time.Duration) time.Time {
	e := shared.entries[key]
	if e == nil {
		e = &entry{}
		shared.entries[key] = e
	}
	e.failureStreak++
	e.successStreak = 0
	e.state = StateCooldown
	e.reason = reason
	e.since = now
	e.lastFailureAt = now
	e.halfOpenInFlight = false
	e.cooldownUntil = now.Add(duration(e.failureStreak))
	return e.cooldownUntil
}

func RecordProviderFailure(channelID int, reason string, now time.Time) time.Time {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	return recordCooldown(providerKey(channelID), reason, now, providerCooldown)
}

func RecordModelFailure(channelID int, model, reason string, now time.Time) time.Time {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	return recordCooldown(modelKey(channelID, model), reason, now, modelCooldown)
}

// EnsureModelFailure records one model-scoped failure unless that exact
// provider-model scope is already in an active cooldown. The fast failover path
// may record capacity evidence before the common runtime updater observes the
// same attempt; suppressing only an already-active cooldown prevents that single
// attempt from advancing the failure streak twice. HALF_OPEN failures are not
// suppressed because a failed real recovery trial must re-enter cooldown.
func EnsureModelFailure(channelID int, model, reason string, now time.Time) time.Time {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	key := modelKey(channelID, model)
	if e := shared.entries[key]; e != nil && e.state == StateCooldown && e.cooldownUntil.After(now) {
		return e.cooldownUntil
	}
	return recordCooldown(key, reason, now, modelCooldown)
}

// RecordModelSuspect records ambiguous evidence without immediately condemning
// the provider. A second ambiguous failure inside a short window escalates the
// channel-model pair into the first model-cooldown stage.
func RecordModelSuspect(channelID int, model, reason string, now time.Time) State {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	key := modelKey(channelID, model)
	e := shared.entries[key]
	if e == nil {
		e = &entry{}
		shared.entries[key] = e
	}
	if e.state == StateSuspect && !e.lastFailureAt.IsZero() && now.Sub(e.lastFailureAt) <= suspectEscalationWindow {
		recordCooldown(key, reason, now, modelCooldown)
		return StateCooldown
	}
	e.state = StateSuspect
	e.reason = reason
	e.since = now
	// A single ambiguous cancellation is soft evidence, not a cooldown-producing
	// failure. Keep the cooldown streak at zero so a second corroborating event
	// enters the first 15-second model cooldown stage rather than jumping to 60s.
	e.failureStreak = 0
	e.successStreak = 0
	e.lastFailureAt = now
	e.cooldownUntil = time.Time{}
	e.halfOpenInFlight = false
	return StateSuspect
}

func recordSuccess(key runtimeKey, now time.Time) {
	e := shared.entries[key]
	if e == nil {
		return
	}
	e.state = StateAvailable
	e.reason = ""
	e.since = now
	e.cooldownUntil = time.Time{}
	e.failureStreak = 0
	e.successStreak++
	e.lastSuccessAt = now
	e.halfOpenInFlight = false
}

func RecordSuccess(channelID int, model string, now time.Time) {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	recordSuccess(providerKey(channelID), now)
	recordSuccess(modelKey(channelID, model), now)
}

func Reset() {
	shared.mu.Lock()
	shared.entries = make(map[runtimeKey]*entry)
	shared.mu.Unlock()
	resetCredentialRuntime()
}
