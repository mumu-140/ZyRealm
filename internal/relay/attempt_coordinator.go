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
// RoutingDecision. Applying these effects remains the caller's responsibility
// in P5A; later slices may centralize application only after behavior parity is
// proven for each transport path.
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

// coordinateAttemptOutcome establishes the P5 coordinator boundary.
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
