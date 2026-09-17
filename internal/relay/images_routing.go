package relay

import (
	"context"
	"errors"
	"strings"

	"github.com/bestruirui/octopus/internal/relay/balancer"
)

// imagesAttemptRoutingResult adapts the Images transport result into the same
// facts-only attempt envelope consumed by RoutingDecision. It does not change
// the public Images error returned to the client.
func imagesAttemptRoutingResult(
	ctx context.Context,
	statusCode int,
	written bool,
	err error,
	span *balancer.AttemptSpan,
) attemptResult {
	result := attemptResult{
		Success:         err == nil,
		Written:         written,
		Err:             err,
		StatusCode:      statusCode,
		UpstreamStatus:  statusCode,
		UpstreamStarted: statusCode >= 200 && statusCode < 300,
		DispatchState:   dispatchMaybeSent,
		traceSpan:       span,
	}
	if err == nil {
		return result
	}

	// Downstream cancellation is health-neutral. Keep the original transport
	// error for active contexts so ambiguous upstream cancellations still flow
	// through the shared RoutingDecision policy rather than being mislabeled as
	// a client cancellation.
	if ctx != nil && ctx.Err() != nil {
		result.Canceled = true
		result.Err = ctx.Err()
		return result
	}

	var upstreamErr *imagesUpstreamError
	if errors.As(err, &upstreamErr) {
		result.RetryAfter = parseRetryAfter(upstreamErr.RetryAfter)
		if upstreamErr.StatusCode > 0 {
			result.UpstreamStatus = upstreamErr.StatusCode
		}
	}

	result.FirstTokenTimeout = strings.Contains(strings.ToLower(err.Error()), "first token timeout")
	return result
}
