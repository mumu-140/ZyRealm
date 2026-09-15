package relay

import (
	"context"
	"errors"
)

var (
	errLocalRelayBudgetExceeded = errors.New("local relay budget exceeded")
	errFirstTokenTimeout        = errors.New("first token timeout")
)

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return ctx.Err()
}

func isLocalRelayBudgetExceeded(ctx context.Context, err error) bool {
	if errors.Is(err, errLocalRelayBudgetExceeded) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errLocalRelayBudgetExceeded)
}

func isFirstTokenTimeout(ctx context.Context, err error) bool {
	if errors.Is(err, errFirstTokenTimeout) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errFirstTokenTimeout)
}

func isManualInterrupt(ctx context.Context, err error) bool {
	if errors.Is(err, errManualInterrupt) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errManualInterrupt)
}

// isClientCancellation only treats the outer request context as authoritative
// evidence that the downstream client canceled or timed out. An upstream,
// transport, proxy, or child context may independently return context.Canceled
// while the client request is still alive; those failures must remain eligible
// for relay failover instead of terminating the request as a client cancel.
func isClientCancellation(ctx context.Context, err error) bool {
	if isManualInterrupt(ctx, err) ||
		isLocalRelayBudgetExceeded(ctx, err) || isLocalRelayBudgetExceeded(ctx, contextError(ctx)) ||
		isFirstTokenTimeout(ctx, err) || isFirstTokenTimeout(ctx, contextError(ctx)) {
		return false
	}
	if ctx == nil {
		return false
	}
	return errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded)
}

// isAmbiguousTransportCancellation identifies a cancellation reported by the
// outbound attempt while the outer client request is still alive. The source
// may be an upstream transport, proxy, adapter, or child context, so this is a
// request-local failover signal rather than proof that the client disconnected
// or that the provider should be globally penalized.
func isAmbiguousTransportCancellation(ctx context.Context, err error) bool {
	if err == nil || isManualInterrupt(ctx, err) || isClientCancellation(ctx, err) ||
		isLocalRelayBudgetExceeded(ctx, err) || isFirstTokenTimeout(ctx, err) {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
