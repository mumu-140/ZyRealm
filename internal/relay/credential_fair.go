package relay

import (
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
)

// selectFairChannelCredential applies credential-local runtime eligibility and
// then charges one equal-weight fair allocation inside the current provider.
// Provider ordering/weights are intentionally outside this function.
func selectFairChannelCredential(
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

	candidates := make([]dbmodel.ChannelKey, 0, len(channel.Keys))
	for _, key := range channel.Keys {
		if key.ID <= 0 || !key.Enabled || key.ChannelKey == "" {
			continue
		}
		if _, excluded := options.ExcludeKeyIDs[key.ID]; excluded {
			continue
		}
		// Credential availability is the sole shared admission authority for all
		// credential revisions. Secret replacement already advances the revision,
		// so a legacy breaker entry must not become a second eligibility gate.
		if !availability.CredentialAvailableRevision(channel.ID, key.ID, key.CredentialRevision, now) {
			options.ExcludeKeyIDs[key.ID] = struct{}{}
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
			continue
		}
		candidates = append(candidates, key)
	}

	return availability.SelectCredentialFair(channel.ID, candidates, options.PreferredKeyID)
}
