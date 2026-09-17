package balancer

import (
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
)

type runtimeCandidateOrder struct {
	Candidates []model.GroupItem
	Decisions  []model.RoutingDecisionEvent
}

func boundedRuntimeDecisionDetail(value string) string {
	value = strings.TrimSpace(value)
	const maxBytes = 256
	if len(value) > maxBytes {
		return value[:maxBytes]
	}
	return value
}

// runtimePolicyCandidates applies strategy ordering after runtime eligibility
// has already been decided by availability. HealthFirst keeps a dedicated
// rotation bucket so runtime requests do not share its cursor with direct
// strategy callers; no strategy contributes an additional admission authority.
func runtimePolicyCandidates(mode model.GroupMode, items []model.GroupItem) []model.GroupItem {
	switch mode {
	case model.GroupModeHealthFirst:
		return healthFirstCandidates(items, "hf-runtime:")
	default:
		return GetBalancer(mode).Candidates(items)
	}
}

// runtimeOrderedCandidatesWithDecisions applies the existing runtime
// eligibility policy and returns observational metadata captured at the same
// decision point. The decision slice is request-local diagnostics only: it does
// not acquire a half-open lease or change the candidate sets handed to the
// configured balancer.
func runtimeOrderedCandidatesWithDecisions(group model.Group, requestModel string, now time.Time) runtimeCandidateOrder {
	primary := make([]model.GroupItem, 0, len(group.Items))
	suspect := make([]model.GroupItem, 0, len(group.Items))
	decisions := make([]model.RoutingDecisionEvent, 0)

	for _, item := range group.Items {
		upstreamModel := ItemUpstreamModel(item, requestModel)
		info := availability.CandidateInfo(item.ChannelID, upstreamModel, now)
		switch info.State {
		case availability.StateCooldown:
			event := model.RoutingDecisionEvent{
				Stage:     model.DecisionStageCandidate,
				Outcome:   model.DecisionOutcomeRejected,
				Reason:    model.DecisionReasonRuntimeCooldown,
				ChannelID: item.ChannelID,
				ModelName: upstreamModel,
				Detail:    boundedRuntimeDecisionDetail(info.Reason),
			}
			if !info.CooldownUntil.IsZero() {
				event.ExpiresAt = info.CooldownUntil.Unix()
			}
			decisions = append(decisions, event)
			continue
		case availability.StateSuspect:
			decisions = append(decisions, model.RoutingDecisionEvent{
				Stage:     model.DecisionStageCandidate,
				Outcome:   model.DecisionOutcomeEligible,
				Reason:    model.DecisionReasonRuntimeSuspect,
				ChannelID: item.ChannelID,
				ModelName: upstreamModel,
				Detail:    boundedRuntimeDecisionDetail(info.Reason),
			})
			suspect = append(suspect, item)
		default:
			// Available and HalfOpen both participate in the configured strategy.
			// HalfOpen single-flight safety is enforced atomically when the relay
			// actually acquires the candidate.
			primary = append(primary, item)
		}
	}

	ordered := make([]model.GroupItem, 0, len(primary)+len(suspect))
	ordered = append(ordered, runtimePolicyCandidates(group.Mode, primary)...)
	ordered = append(ordered, runtimePolicyCandidates(group.Mode, suspect)...)
	return runtimeCandidateOrder{Candidates: ordered, Decisions: decisions}
}

func runtimeOrderedCandidates(group model.Group, requestModel string, now time.Time) []model.GroupItem {
	return runtimeOrderedCandidatesWithDecisions(group, requestModel, now).Candidates
}

func runtimeStickyEligible(item model.GroupItem, requestModel string, now time.Time) bool {
	return availability.CandidateState(
		item.ChannelID,
		ItemUpstreamModel(item, requestModel),
		now,
	) == availability.StateAvailable
}
