package relay

import (
	"net/http"
	"strconv"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/gin-gonic/gin"
)

// nearestRuntimeRecovery returns the earliest point at which any candidate in
// this group can leave an active shared runtime cooldown. It returns false when
// even one item is not currently cooling, because in that case an empty iterator
// may have another cause and a runtime Retry-After would be misleading.
func nearestRuntimeRecovery(items []dbmodel.GroupItem, requestModel string, now time.Time) (time.Time, bool) {
	if len(items) == 0 {
		return time.Time{}, false
	}

	var nearest time.Time
	for _, item := range items {
		upstreamModel := balancer.ItemUpstreamModel(item, requestModel)
		info := availability.CandidateInfo(item.ChannelID, upstreamModel, now)
		if info.State != availability.StateCooldown || !info.CooldownUntil.After(now) {
			return time.Time{}, false
		}
		if nearest.IsZero() || info.CooldownUntil.Before(nearest) {
			nearest = info.CooldownUntil
		}
	}
	return nearest, !nearest.IsZero()
}

func retryAfterSecondsUntil(until, now time.Time) int {
	d := until.Sub(now)
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

func writeRuntimeCooldownUnavailable(c *gin.Context, items []dbmodel.GroupItem, requestModel string) bool {
	now := time.Now()
	until, ok := nearestRuntimeRecovery(items, requestModel, now)
	if !ok {
		return false
	}
	if seconds := retryAfterSecondsUntil(until, now); seconds > 0 {
		c.Header("Retry-After", strconv.Itoa(seconds))
	}
	resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "all eligible channels are cooling down")
	return true
}
