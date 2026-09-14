package relay

import (
	"errors"
	"fmt"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
)

const (
	defaultMaxProviderAttempts           = 20
	defaultMaxWireAttempts               = 20
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
	maxProviders       int
	maxWires           int
	maxUnknownReplays  int
	providers          map[int]struct{}
	providerAttemptNum map[int]int
	wires              int
	unknownReplayCount int
	traceSpan          *balancer.AttemptSpan
}

func newRelayAttemptBudget() *relayAttemptBudget {
	return newRelayAttemptBudgetWithLimits(configuredMaxProviderAttempts(), configuredMaxWireAttempts())
}

func configuredMaxProviderAttempts() int {
	return configuredPositiveAttemptLimit(dbmodel.SettingKeyRelayMaxProviderAttempts, defaultMaxProviderAttempts)
}

func configuredMaxWireAttempts() int {
	return configuredPositiveAttemptLimit(dbmodel.SettingKeyRelayMaxWireAttempts, defaultMaxWireAttempts)
}

func configuredPositiveAttemptLimit(key dbmodel.SettingKey, fallback int) int {
	value, err := op.SettingGetInt(key)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func newRelayAttemptBudgetWithLimits(maxProviders, maxWires int) *relayAttemptBudget {
	if maxProviders <= 0 {
		maxProviders = defaultMaxProviderAttempts
	}
	if maxWires <= 0 {
		maxWires = defaultMaxWireAttempts
	}
	return &relayAttemptBudget{
		maxProviders:       maxProviders,
		maxWires:           maxWires,
		maxUnknownReplays:  defaultMaxUnknownCrossProviderReplay,
		providers:          make(map[int]struct{}),
		providerAttemptNum: make(map[int]int),
	}
}

func (b *relayAttemptBudget) bindTraceSpan(span *balancer.AttemptSpan) {
	if b == nil {
		return
	}
	b.traceSpan = span
}

func (b *relayAttemptBudget) markStop(reason failoverStopReason) {
	if b == nil || b.traceSpan == nil || reason == "" {
		return
	}
	b.traceSpan.SetFailoverStopReason(string(reason))
}

// canUseProvider is a non-mutating preflight used before acquiring local
// concurrency/RPM capacity. The budget is keyed by channel ID: a previously
// charged upstream channel remains usable until the wire budget is exhausted;
// a new channel is rejected after the configured distinct-upstream limit has
// been reached.
func (b *relayAttemptBudget) canUseProvider(channelID int) bool {
	if b == nil {
		return true
	}
	if _, seen := b.providers[channelID]; seen {
		return true
	}
	return len(b.providers) < b.maxProviders
}

// tryStartWire charges one execution attempt. An upstream channel is charged
// once per request even when multiple credentials/protocol plans are attempted
// inside it.
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
		b.providerAttemptNum[channelID] = len(b.providers)
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
		b.markStop(failoverStopUnknownReplayBudget)
		return false
	}
	b.unknownReplayCount++
	return true
}

func (b *relayAttemptBudget) wireExhausted() bool {
	return b != nil && b.wires >= b.maxWires
}

func (b *relayAttemptBudget) wireAttemptIndex() int {
	if b == nil {
		return 0
	}
	return b.wires
}

func (b *relayAttemptBudget) providerAttemptIndex(channelID int) int {
	if b == nil {
		return 0
	}
	return b.providerAttemptNum[channelID]
}

func isRelayAttemptBudgetExceeded(err error) bool {
	return errors.Is(err, errRelayProviderAttemptsExceeded) || errors.Is(err, errRelayWireAttemptsExceeded)
}

func isProviderAttemptBudgetExceeded(err error) bool {
	return errors.Is(err, errRelayProviderAttemptsExceeded)
}

func attemptBudgetFailoverStopReason(err error) (failoverStopReason, bool) {
	switch {
	case errors.Is(err, errRelayProviderAttemptsExceeded):
		return failoverStopProviderAttemptBudget, true
	case errors.Is(err, errRelayWireAttemptsExceeded):
		return failoverStopWireAttemptBudget, true
	default:
		return "", false
	}
}
