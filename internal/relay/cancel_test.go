package relay

import (
	"context"
	"fmt"
	"testing"
)

func TestIsClientCancellationDoesNotTrustWrappedTransportCancellation(t *testing.T) {
	ctx := context.Background()

	if isClientCancellation(ctx, fmt.Errorf("failed to send request: %w", context.Canceled)) {
		t.Fatalf("expected wrapped context.Canceled with a live outer context to remain failover-eligible")
	}
	if isClientCancellation(ctx, fmt.Errorf("failed to send request: %w", context.DeadlineExceeded)) {
		t.Fatalf("expected wrapped context.DeadlineExceeded with a live outer context to remain failover-eligible")
	}
}

func TestIsClientCancellationUsesOuterContextState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if !isClientCancellation(ctx, fmt.Errorf("upstream request aborted")) {
		t.Fatalf("expected canceled outer request context to be treated as client cancellation")
	}
}

func TestIsClientCancellationUsesOuterDeadlineState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	<-ctx.Done()
	if !isClientCancellation(ctx, fmt.Errorf("upstream request aborted")) {
		t.Fatalf("expected expired outer request context to be treated as client cancellation")
	}
}

func TestIsClientCancellationIgnoresOrdinaryErrors(t *testing.T) {
	if isClientCancellation(context.Background(), fmt.Errorf("dial tcp timeout")) {
		t.Fatalf("expected ordinary upstream error to not be treated as client cancellation")
	}
}

func TestIsClientCancellationIgnoresLocalRelayBudgetTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeoutCause(context.Background(), 0, errLocalRelayBudgetExceeded)
	defer cancel()

	<-ctx.Done()
	if isClientCancellation(ctx, contextError(ctx)) {
		t.Fatalf("expected local relay budget timeout to not be treated as client cancellation")
	}
}

func TestIsClientCancellationIgnoresFirstTokenTimeout(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errFirstTokenTimeout)

	if isClientCancellation(ctx, contextError(ctx)) {
		t.Fatalf("expected first-token timeout to not be treated as client cancellation")
	}
}

func TestIsAmbiguousTransportCancellationRequiresLiveOuterContext(t *testing.T) {
	wrapped := fmt.Errorf("failed to send request: %w", context.Canceled)
	if !isAmbiguousTransportCancellation(context.Background(), wrapped) {
		t.Fatalf("expected wrapped cancellation with live outer context to be ambiguous transport cancellation")
	}

	doubleWrapped := fmt.Errorf("channel relay-a failed: %w", wrapped)
	if !isAmbiguousTransportCancellation(context.Background(), doubleWrapped) {
		t.Fatalf("expected cancellation identity to survive the channel-level failure wrapper")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if isAmbiguousTransportCancellation(ctx, wrapped) {
		t.Fatalf("expected canceled outer context to take precedence over ambiguous transport cancellation")
	}
}

func TestIsAmbiguousTransportCancellationExcludesLocalTimeoutCauses(t *testing.T) {
	if isAmbiguousTransportCancellation(context.Background(), errFirstTokenTimeout) {
		t.Fatalf("expected first-token timeout to stay out of ambiguous transport cancellation")
	}
	if isAmbiguousTransportCancellation(context.Background(), errLocalRelayBudgetExceeded) {
		t.Fatalf("expected local relay budget timeout to stay out of ambiguous transport cancellation")
	}
}
