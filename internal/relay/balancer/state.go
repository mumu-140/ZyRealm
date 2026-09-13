package balancer

import (
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/availability"
)

func init() {
	op.RegisterRelayBalancerStateReset(ResetStateByChannel)
}

func ResetStateByChannel(channelID int) {
	resetCircuitBreakerByChannel(channelID)
	resetStickyByChannel(channelID)
	resetConcurrencyByChannel(channelID)
	resetRateByChannel(channelID)
	availability.ResetChannel(channelID)
}
