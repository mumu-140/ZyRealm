package relay

import (
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
)

// selectOrderedAvailableCredential preserves Channel.GetChannelKey ordering
// (preferred key, then lowest TotalCost) while making credential availability
// the sole shared admission authority. The caller-owned exclusion map is updated
// in place so retries keep skipping cooling credentials.
func selectOrderedAvailableCredential(
	channel *dbmodel.Channel,
	options dbmodel.ChannelKeySelectOptions,
	iterator *balancer.Iterator,
	now time.Time,
) dbmodel.ChannelKey {
	if channel == nil {
		return dbmodel.ChannelKey{}
	}
	if options.ExcludeKeyIDs == nil {
		options.ExcludeKeyIDs = make(map[int]struct{})
	}

	for {
		key := channel.GetChannelKey(options)
		if key.ChannelKey == "" {
			return dbmodel.ChannelKey{}
		}
		if availability.CredentialAvailableRevision(channel.ID, key.ID, key.CredentialRevision, now) {
			return key
		}

		options.ExcludeKeyIDs[key.ID] = struct{}{}
		if options.PreferredKeyID == key.ID {
			options.PreferredKeyID = 0
		}
		if iterator != nil {
			iterator.RecordDecision(dbmodel.RoutingDecisionEvent{
				Stage:        dbmodel.DecisionStageCredential,
				Outcome:      dbmodel.DecisionOutcomeRejected,
				Reason:       dbmodel.DecisionReasonCredentialCooldown,
				ChannelID:    channel.ID,
				ChannelKeyID: key.ID,
				ChannelName:  channel.Name,
			})
		}
	}
}
