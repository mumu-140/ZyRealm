package relay

// attemptDisposition is the control-flow projection of an already-computed
// RoutingDecision. It contains no classification logic and owns no retry budget,
// candidate selection, credential selection, or transport behavior.
type attemptDisposition struct {
	Directive           routingDirective
	SkipProvider        bool
	RetrySameCredential bool
	Terminal            bool
	ReplaySafety        routingReplaySafety
}

// attemptEffectPlan is the side-effect projection of an already-computed
// RoutingDecision. The coordinator remains a pure projection; each transport
// boundary applies the projected effects explicitly after its wire attempt.
type attemptEffectPlan struct {
	RuntimeEffect          routingRuntimeEffect
	OutlierScope           failureScope
	RouteLearningCandidate bool
	CredentialFailure      bool
	ContentPolicy          bool
}

type attemptCoordination struct {
	Disposition attemptDisposition
	Effects     attemptEffectPlan
}

// coordinateAttemptOutcome establishes the transport-neutral coordinator boundary.
//
// The input MUST already contain a valid RoutingDecision. This function is a
// pure projection: it never synthesizes, reclassifies, or overrides routing
// policy from raw status codes, errors, or transport facts.
func coordinateAttemptOutcome(result attemptResult) (attemptCoordination, bool) {
	decision := result.Decision
	if !decision.Valid {
		return attemptCoordination{}, false
	}

	return attemptCoordination{
		Disposition: attemptDisposition{
			Directive:           decision.Directive,
			SkipProvider:        decision.SkipProvider,
			RetrySameCredential: decision.RetrySameCredential,
			Terminal:            decision.Terminal,
			ReplaySafety:        decision.ReplaySafety,
		},
		Effects: attemptEffectPlan{
			RuntimeEffect:          decision.RuntimeEffect,
			OutlierScope:           decision.OutlierScope,
			RouteLearningCandidate: decision.RouteLearningCandidate,
			CredentialFailure:      decision.Domain == failureDomainCredential,
			ContentPolicy:          decision.ContentPolicy,
		},
	}, true
}
