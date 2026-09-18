package relay

type sidepathDirectiveAction uint8

const (
	sidepathDirectiveStop sidepathDirectiveAction = iota
	sidepathDirectiveRetrySameCredential
	sidepathDirectiveRotateCredential
	sidepathDirectiveNextProvider
)

// resolveSidepathDirective adapts the canonical coordinator disposition to the
// execution shapes available to Images, Compact, and WebSocket. It only maps an
// already-computed directive; retry ceilings remain owned by each caller.
func resolveSidepathDirective(disposition attemptDisposition) sidepathDirectiveAction {
	switch disposition.Directive {
	case routingDirectiveRetrySameCredential:
		return sidepathDirectiveRetrySameCredential
	case routingDirectiveRotateCredential:
		return sidepathDirectiveRotateCredential
	case routingDirectiveNextProvider, routingDirectiveNextCandidate, routingDirectiveProtocolOrProvider:
		return sidepathDirectiveNextProvider
	case routingDirectiveComplete, routingDirectiveTerminal:
		return sidepathDirectiveStop
	default:
		return sidepathDirectiveStop
	}
}
