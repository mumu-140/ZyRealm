package relay

import (
	"context"
	"errors"
	"testing"
)

func TestRoutingDecisionFirstTokenTimeoutMaybeSentIsUnknownOutcome(t *testing.T) {
	result := attemptResult{
		Err:               errors.New("first token timeout"),
		FirstTokenTimeout: true,
		DispatchState:     dispatchMaybeSent,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if decision.ReplaySafety != routingReplayUnknownOutcome {
		t.Fatalf("replay safety=%q, want unknown upstream outcome", decision.ReplaySafety)
	}
	if decision.Directive != routingDirectiveNextProvider || !decision.SkipProvider {
		t.Fatalf("directive=%q skipProvider=%t, want next provider", decision.Directive, decision.SkipProvider)
	}
}

func TestRoutingDecisionFirstTokenTimeoutNotSentDoesNotSpendUnknownReplay(t *testing.T) {
	result := attemptResult{
		Err:               errors.New("first token timeout"),
		FirstTokenTimeout: true,
		DispatchState:     dispatchNotSent,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if decision.ReplaySafety != routingReplayNotSent {
		t.Fatalf("replay safety=%q, want not_sent", decision.ReplaySafety)
	}
}
