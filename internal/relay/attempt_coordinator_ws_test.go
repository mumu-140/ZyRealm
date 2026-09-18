package relay

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func p5bSource(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve P5B source path")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func TestP5BWSConsumesCoordinatorEffectsAndPreservesSessionBoundary(t *testing.T) {
	source := p5bSource(t, "ws_client.go")

	for _, required := range []string{
		"coordinateAttemptOutcome(result)",
		"applyRuntimeAvailabilityEffect(",
		"coordination.Effects.CredentialFailure",
		"coordination.Effects.OutlierScope",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("WS coordinator migration missing %q", required)
		}
	}

	for _, retired := range []string{
		"decision.Domain == failureDomainCredential",
		"decision.OutlierScope",
	} {
		if strings.Contains(source, retired) {
			t.Fatalf("WS still consumes RoutingDecision effect directly via %q", retired)
		}
	}

	// P5D1 may converge retry direction, but these WS session/replay gates remain transport-owned.
	for _, invariant := range []string{
		"resolveSidepathDirective(coordination.Disposition)",
		"budget := 15 * time.Second",
		"maxChannelAttempts > 3",
		"result.ResetConversation",
	} {
		if !strings.Contains(source, invariant) {
			t.Fatalf("P5B changed WS retry/session invariant %q", invariant)
		}
	}

	transportSource := p5bSource(t, "relay_websocket.go")
	if !strings.Contains(transportSource, "retryViaFreshUpstreamWS") {
		t.Fatal("P5B must preserve WS reconnect/redial transport path")
	}
}

func TestP5BCoreConsumesCoordinatorRuntimeCredentialAndRouteLearningEffects(t *testing.T) {
	source := p5bSource(t, "relay_handler.go")

	for _, required := range []string{
		"applyRuntimeAvailabilityEffect(",
		"coordination.Effects.CredentialFailure",
		"coordination.Effects.RouteLearningCandidate",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Core coordinator effect migration missing %q", required)
		}
	}

	if strings.Contains(source, "recordRuntimeAvailabilityEvidence(") {
		t.Fatal("Core must apply runtime availability from the coordinator effect plan")
	}
	if strings.Contains(source, "result.Decision.Domain == failureDomainCredential") {
		t.Fatal("Core credential rotation must consume the coordinator credential-failure effect")
	}
}

