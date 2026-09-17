package relay

import (
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/protocolroute"
	"github.com/bestruirui/octopus/internal/relay/availability"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

type capabilityPlanRejection struct {
	Plan *protocolroute.AttemptPlan
	Info availability.CapabilitySnapshot
}

type capabilityFilterResult struct {
	Filtered   []*protocolroute.AttemptPlan
	Rejected   []capabilityPlanRejection
	Nearest    availability.CapabilitySnapshot
	BlockedAny bool
}

// filterCapabilityNegativePlansDetailed retains immutable rejection evidence
// while applying the narrowest active suppression scope for each plan.
func filterCapabilityNegativePlansDetailed(
	channel *dbmodel.Channel,
	request *transformerModel.InternalLLMRequest,
	plans []*protocolroute.AttemptPlan,
	now time.Time,
) capabilityFilterResult {
	if len(plans) == 0 || channel == nil || request == nil {
		return capabilityFilterResult{Filtered: plans}
	}

	result := capabilityFilterResult{
		Filtered: make([]*protocolroute.AttemptPlan, 0, len(plans)),
		Rejected: make([]capabilityPlanRejection, 0),
	}
	for _, plan := range plans {
		if plan == nil {
			continue
		}
		info := capabilitySuppressionInfo(channel, request, plan, now)
		if !info.Blocked {
			result.Filtered = append(result.Filtered, plan)
			continue
		}

		result.BlockedAny = true
		result.Rejected = append(result.Rejected, capabilityPlanRejection{Plan: plan, Info: info})
		if result.Nearest.ExpiresAt.IsZero() || info.ExpiresAt.Before(result.Nearest.ExpiresAt) {
			result.Nearest = info
		}
	}
	return result
}

func capabilityNegativeDecisionEvent(
	channel *dbmodel.Channel,
	key dbmodel.ChannelKey,
	rejection capabilityPlanRejection,
) dbmodel.RoutingDecisionEvent {
	event := dbmodel.RoutingDecisionEvent{
		Stage:        dbmodel.DecisionStageProtocol,
		Outcome:      dbmodel.DecisionOutcomeRejected,
		Reason:       dbmodel.DecisionReasonCapabilityNegative,
		ChannelKeyID: key.ID,
	}
	if channel != nil {
		event.ChannelID = channel.ID
		event.ChannelName = channel.Name
	}
	if rejection.Plan != nil {
		event.ModelName = rejection.Plan.UpstreamModel()
		event.Protocol = string(rejection.Plan.UpstreamProtocol())
	}
	if rejection.Info.Blocked && !rejection.Info.ExpiresAt.IsZero() {
		event.ExpiresAt = rejection.Info.ExpiresAt.Unix()
	}
	return event
}
