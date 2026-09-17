package relay

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/relay/balancer"
)

// compactAttemptRoutingResult adapts the compact sidepath's final upstream
// outcome into the same attemptResult/RoutingDecision contract used by the main
// relay path. Compact buffers the upstream response before committing a 2xx
// downstream body, so failures passed here are not downstream-committed.
func compactAttemptRoutingResult(
	ctx context.Context,
	request *relayRequest,
	channelID int,
	statusCode int,
	retryAfter time.Duration,
	err error,
	span *balancer.AttemptSpan,
) attemptResult {
	result := attemptResult{
		Success:         err == nil,
		StatusCode:      statusCode,
		UpstreamStatus:  statusCode,
		RetryAfter:      retryAfter,
		Err:             err,
		UpstreamStarted: statusCode > 0,
		DispatchState:   dispatchMaybeSent,
		traceSpan:       span,
	}
	if err != nil && ctx != nil && ctx.Err() != nil {
		result.Canceled = true
		result.Err = ctx.Err()
	}
	return withRoutingDecision(ctx, request, channelID, result)
}
