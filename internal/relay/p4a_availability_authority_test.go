package relay

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestRoutingDecisionGeneric500StaysRuntimeNeutral(t *testing.T) {
	result := attemptResult{
		Err:        errors.New("channel failed"),
		StatusCode: http.StatusInternalServerError,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 1, result)
	if decision.Domain != failureDomainUnknown {
		t.Fatalf("domain = %v, want unknown", decision.Domain)
	}
	if decision.RuntimeEffect != routingRuntimeNone {
		t.Fatalf("runtime effect = %q, want none", decision.RuntimeEffect)
	}
	if decision.OutlierScope != scopeChannel {
		t.Fatalf("outlier scope = %v, want channel statistical evidence", decision.OutlierScope)
	}
	if !decision.RouteLearningCandidate {
		t.Fatal("generic 5xx must remain eligible for managed-route learning")
	}
}
