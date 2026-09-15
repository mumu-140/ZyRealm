package relay

import (
	"context"
	"testing"
)

func manualInterruptTestContext() context.Context {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errManualInterrupt)
	return ctx
}

func assertManualInterruptDecision(t *testing.T, result attemptResult, wantReplay routingReplaySafety) {
	t.Helper()
	decision := decideRoutingAttempt(manualInterruptTestContext(), nil, 1, result)
	if decision.RuleID != "manual_interrupt" {
		t.Fatalf("rule=%q, want manual_interrupt", decision.RuleID)
	}
	if decision.FailureScope != routingScopeRequest {
		t.Fatalf("scope=%q, want request", decision.FailureScope)
	}
	if decision.Directive != routingDirectiveTerminal || !decision.Terminal {
		t.Fatalf("directive=%q terminal=%t, want terminal", decision.Directive, decision.Terminal)
	}
	if decision.RuntimeEffect != routingRuntimeNone {
		t.Fatalf("runtime effect=%q, want none", decision.RuntimeEffect)
	}
	if decision.OutlierScope != scopeIgnore {
		t.Fatalf("outlier scope=%v, want ignore", decision.OutlierScope)
	}
	if decision.CircuitEffect != "none" {
		t.Fatalf("circuit effect=%q, want none", decision.CircuitEffect)
	}
	if decision.SkipProvider || decision.RetrySameCredential {
		t.Fatalf("manual interrupt must not retry/failover: skipProvider=%t retrySame=%t", decision.SkipProvider, decision.RetrySameCredential)
	}
	if decision.ReplaySafety != wantReplay {
		t.Fatalf("replay safety=%q, want %q", decision.ReplaySafety, wantReplay)
	}
}

func TestManualInterruptDecisionPreDispatchIsNotSent(t *testing.T) {
	assertManualInterruptDecision(t, attemptResult{
		Err:           context.Canceled,
		DispatchState: dispatchNotSent,
	}, routingReplayNotSent)
}

func TestManualInterruptDecisionMaybeSentTracksUnknownOutcome(t *testing.T) {
	assertManualInterruptDecision(t, attemptResult{
		Err:           context.Canceled,
		DispatchState: dispatchMaybeSent,
	}, routingReplayUnknownOutcome)
}

func TestManualInterruptDecisionCommittedRemainsCommitted(t *testing.T) {
	assertManualInterruptDecision(t, attemptResult{
		Err:           context.Canceled,
		Written:       true,
		DispatchState: dispatchMaybeSent,
	}, routingReplayCommitted)
}
