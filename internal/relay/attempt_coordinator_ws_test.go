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

func TestP5BWSConsumesCoordinatorEffectsWithoutRetryMigration(t *testing.T) {
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

	// These transport/session gates are intentionally NOT migrated in P5B.
	for _, invariant := range []string{
		"isRetryableStatus(result.StatusCode)",
		"budget := 15 * time.Second",
		"maxChannelAttempts > 3",
		"result.ResetConversation",
		"retryViaFreshUpstreamWS",
	} {
		if !strings.Contains(source, invariant) {
			t.Fatalf("P5B changed WS retry/session invariant %q", invariant)
		}
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

func TestP5BSidepathsRemainOutOfCoordinator(t *testing.T) {
	for _, name := range []string{"images.go", "compact.go"} {
		source := p5bSource(t, name)
		if strings.Contains(source, "coordinateAttemptOutcome(") {
			t.Fatalf("%s must remain outside AttemptCoordinator in P5B", name)
		}
	}
}
