package relay

import (
	"encoding/json"
	"strings"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
)

func TestP4C3ARouteLearningPolicyReplacesCircuitEffect(t *testing.T) {
	candidate := RoutingDecision{
		Valid:               true,
		RouteLearningEffect: routingRouteLearningCandidate,
	}
	if !shouldLearnManagedRoute(candidate, false, 500) {
		t.Fatal("route-learning candidate 500 should remain eligible")
	}
	if shouldLearnManagedRoute(candidate, true, 503) {
		t.Fatal("retry-enabled passthrough 503 must remain ineligible")
	}

	none := RoutingDecision{
		Valid:               true,
		RouteLearningEffect: routingRouteLearningNone,
	}
	if shouldLearnManagedRoute(none, false, 500) {
		t.Fatal("route-learning none must remain ineligible")
	}
}

func TestP4C3ANewAttemptTraceUsesRouteLearningEffect(t *testing.T) {
	encoded, err := json.Marshal(dbmodel.AttemptRoutingTrace{
		RouteLearningEffect: string(routingRouteLearningCandidate),
	})
	if err != nil {
		t.Fatalf("marshal routing trace: %v", err)
	}
	body := string(encoded)
	if !strings.Contains(body, `"route_learning_effect":"candidate"`) {
		t.Fatalf("route-learning trace field missing: %s", body)
	}
	if strings.Contains(body, "circuit_effect") {
		t.Fatalf("new routing trace must not emit circuit_effect: %s", body)
	}
}

func TestP4C3ARouteLearningTraceBumpsDecisionVersion(t *testing.T) {
	if dbmodel.RoutingDecisionTraceVersion != "routing-decisions-v2" {
		t.Fatalf("decision trace version = %q, want routing-decisions-v2", dbmodel.RoutingDecisionTraceVersion)
	}
}
