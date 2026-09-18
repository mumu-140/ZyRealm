package relay

import (
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "testing"
)

func TestAttemptCoordinatorConsumesPrecomputedDecisionWithoutReclassification(t *testing.T) {
    // Deliberately make the raw result disagree with the attached decision.
    // The coordinator boundary must trust the already-computed policy verdict.
    result := attemptResult{
        Success:    true,
        StatusCode: 200,
        Decision: RoutingDecision{
            Valid:                  true,
            Domain:                 failureDomainProviderTransient,
            Directive:              routingDirectiveNextProvider,
            RuntimeEffect:          routingRuntimeProviderCooldown,
            OutlierScope:           scopeChannel,
            RouteLearningCandidate: true,
            ReplaySafety:           routingReplayUnknownOutcome,
            SkipProvider:           true,
        },
    }

    got, ok := coordinateAttemptOutcome(result)
    if !ok {
        t.Fatal("valid RoutingDecision must produce a coordination plan")
    }

    if got.Disposition.Directive != routingDirectiveNextProvider {
        t.Fatalf("directive=%q, want next_provider", got.Disposition.Directive)
    }
    if !got.Disposition.SkipProvider {
        t.Fatal("skip-provider flag was not preserved")
    }
    if got.Disposition.ReplaySafety != routingReplayUnknownOutcome {
        t.Fatalf("replay safety=%q, want unknown_outcome", got.Disposition.ReplaySafety)
    }
    if got.Effects.RuntimeEffect != routingRuntimeProviderCooldown {
        t.Fatalf("runtime effect=%q, want provider_cooldown", got.Effects.RuntimeEffect)
    }
    if got.Effects.OutlierScope != scopeChannel {
        t.Fatalf("outlier scope=%v, want provider/channel scope", got.Effects.OutlierScope)
    }
    if !got.Effects.RouteLearningCandidate {
        t.Fatal("route-learning candidate flag was not preserved")
    }
    if got.Effects.CredentialFailure {
        t.Fatal("provider failure must not be projected as credential failure")
    }
}

func TestAttemptCoordinatorProjectsCredentialAndTerminalEffects(t *testing.T) {
    result := attemptResult{
        Decision: RoutingDecision{
            Valid:               true,
            Domain:              failureDomainCredential,
            Directive:           routingDirectiveRotateCredential,
            RuntimeEffect:       routingRuntimeCredentialCooldown,
            OutlierScope:        scopeIgnore,
            ReplaySafety:        routingReplayNotSent,
            RetrySameCredential: false,
            Terminal:            false,
            ContentPolicy:       true,
        },
    }

    got, ok := coordinateAttemptOutcome(result)
    if !ok {
        t.Fatal("valid RoutingDecision must produce a coordination plan")
    }
    if got.Disposition.Directive != routingDirectiveRotateCredential {
        t.Fatalf("directive=%q, want rotate_credential", got.Disposition.Directive)
    }
    if !got.Effects.CredentialFailure {
        t.Fatal("credential domain must project credential-failure effect")
    }
    if !got.Effects.ContentPolicy {
        t.Fatal("content-policy flag must be preserved without reinterpretation")
    }
    if got.Disposition.Terminal {
        t.Fatal("terminal flag changed during projection")
    }
}

func TestAttemptCoordinatorRejectsUnclassifiedOutcome(t *testing.T) {
    if _, ok := coordinateAttemptOutcome(attemptResult{}); ok {
        t.Fatal("coordinator must not synthesize a RoutingDecision")
    }
}

func TestAttemptCoordinatorSourceDoesNotReclassifyRouting(t *testing.T) {
    _, thisFile, _, ok := runtime.Caller(0)
    if !ok {
        t.Fatal("resolve test source path")
    }
    path := filepath.Join(filepath.Dir(thisFile), "attempt_coordinator.go")
    data, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("read coordinator source: %v", err)
    }
    source := string(data)
    for _, forbidden := range []string{
        "withRoutingDecision(",
        "decideRoutingAttempt(",
        "classifyRoutingFailure(",
        "fallbackStatus(",
        "isRetryableStatus(",
        "outlierErrorText(",
    } {
        if strings.Contains(source, forbidden) {
            t.Fatalf("attempt coordinator must not reclassify routing via %q", forbidden)
        }
    }
}
