package relay

import (
	"errors"
	"fmt"
)

const (
	defaultMaxProviderAttempts           = 4
	defaultMaxWireAttempts               = 8
	defaultMaxUnknownCrossProviderReplay = 1
)

var (
	errRelayProviderAttemptsExceeded = fmt.Errorf("%w: provider attempt budget exceeded", errLocalRelayBudgetExceeded)
	errRelayWireAttemptsExceeded     = fmt.Errorf("%w: wire attempt budget exceeded", errLocalRelayBudgetExceeded)
)

// relayAttemptBudget is request-local and intentionally independent of the
// configured balancing mode. Local skips (capacity, circuit, disabled channel)
// do not consume it; a budget slot is charged only when a real relay attempt is
// about to start.
type relayAttemptBudget struct {
	maxProviders         int
	maxWires             int
	maxUnknownReplays    int
	providers            map[int]struct{}
	wires                int
	unknownReplayCount   int
}

func newRelayAttemptBudget() *relayAttemptBudget {
	return newRelayAttemptBudgetWithLimits(defaultMaxProviderAttempts, defaultMaxWireAttempts)
}

func newRelayAttemptBudgetWithLimits(maxProviders, maxWires int) *relayAttemptBudget {
	if maxProviders <= 0 {
		maxProviders = defaultMaxProviderAttempts
	}
	if maxWires <= 0 {
		maxWires = defaultMaxWireAttempts
	}
	return &relayAttemptBudget{
		maxProviders:      maxProviders,
		maxWires:          maxWires,
		maxUnknownReplays: defaultMaxUnknownCrossProviderReplay,
		providers:         make(map[int]struct{}),
	}
}

// canUseProvider is a non-mutating preflight used before acquiring local
// concurrency/RPM capacity. A previously charged provider remains usable until
// the wire budget is exhausted; a new provider is rejected after the unique
// provider limit has been reached.
func (b *relayAttemptBudget) canUseProvider(channelID int) bool {
	if b == nil {
		return true
	}
	if _, seen := b.providers[channelID]; seen {
		return true
	}
	return len(b.providers) < b.maxProviders
}

// tryStartWire charges one execution attempt. A provider is charged once per
// request even when multiple credentials/protocol plans are attempted inside it.
func (b *relayAttemptBudget) tryStartWire(channelID int) error {
	if b == nil {
		return nil
	}
	if b.wires >= b.maxWires {
		return errRelayWireAttemptsExceeded
	}
	if _, seen := b.providers[channelID]; !seen {
		if !b.canUseProvider(channelID) {
			return errRelayProviderAttemptsExceeded
		}
		b.providers[channelID] = struct{}{}
	}
	b.wires++
	return nil
}

// tryUnknownCrossProviderReplay charges one replay whose upstream outcome is
// unknown (for example an outbound context cancellation while the client is
// still alive). Ordinary provider rejections and NOT_SENT transport failures do
// not consume this separate safety budget.
func (b *relayAttemptBudget) tryUnknownCrossProviderReplay() bool {
	if b == nil {
		return true
	}
	if b.unknownReplayCount >= b.maxUnknownReplays {
		return false
	}
	b.unknownReplayCount++
	return true
}

func (b *relayAttemptBudget) wireExhausted() bool {
	return b != nil && b.wires >= b.maxWires
}

func isRelayAttemptBudgetExceeded(err error) bool {
	return errors.Is(err, errRelayProviderAttemptsExceeded) || errors.Is(err, errRelayWireAttemptsExceeded)
}

func isProviderAttemptBudgetExceeded(err error) bool {
	return errors.Is(err, errRelayProviderAttemptsExceeded)
}
