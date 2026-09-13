package balancer

import (
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
)

// runtimeOrderedCandidates applies shared runtime eligibility before the
// configured balancing algorithm. Available and passive-half-open candidates
// stay in the normal strategy pool so an expired cooldown can eventually
// recover under real traffic. Suspect candidates are retained as a fallback
// tier. Active cooldown candidates are excluded entirely.
func runtimeOrderedCandidates(group model.Group, requestModel string, now time.Time) []model.GroupItem {
	primary := make([]model.GroupItem, 0, len(group.Items))
	suspect := make([]model.GroupItem, 0, len(group.Items))

	for _, item := range group.Items {
		upstreamModel := ItemUpstreamModel(item, requestModel)
		switch availability.CandidateState(item.ChannelID, upstreamModel, now) {
		case availability.StateCooldown:
			continue
		case availability.StateSuspect:
			suspect = append(suspect, item)
		default:
			// Available and HalfOpen both participate in the configured strategy.
			// HalfOpen single-flight safety is enforced atomically when the relay
			// actually acquires the candidate.
			primary = append(primary, item)
		}
	}

	b := GetBalancer(group.Mode)
	ordered := make([]model.GroupItem, 0, len(primary)+len(suspect))
	ordered = append(ordered, b.Candidates(primary)...)
	ordered = append(ordered, b.Candidates(suspect)...)
	return ordered
}

func runtimeStickyEligible(item model.GroupItem, requestModel string, now time.Time) bool {
	return availability.CandidateState(
		item.ChannelID,
		ItemUpstreamModel(item, requestModel),
		now,
	) == availability.StateAvailable
}
