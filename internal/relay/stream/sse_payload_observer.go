package stream

// SSEPayloadObserver incrementally classifies raw Server-Sent Events without
// changing or buffering the bytes that are forwarded to the client. It keeps
// only bounded line-prefix state so passthrough can preserve byte fidelity even
// for very large data events.
//
// A payload is considered semantic only after a complete SSE event boundary is
// observed and that event contained a non-empty data field. Comment-only
// keepalives, id/retry/event metadata, and incomplete events do not count.
type SSEPayloadObserver struct {
	linePrefix        [5]byte
	linePrefixLen     int
	lineLen           int
	lineValueContent  bool
	eventHasPayload   bool
	swallowLineFeed   bool
}

// NewSSEPayloadObserver creates a bounded, stateful observer suitable for raw
// SSE chunks, including chunks that split CRLF or field names across reads.
func NewSSEPayloadObserver() *SSEPayloadObserver {
	return &SSEPayloadObserver{}
}

// Observe consumes raw SSE bytes and reports whether this chunk completed at
// least one semantic data-bearing event. The caller remains responsible for
// forwarding the original chunk unchanged.
func (o *SSEPayloadObserver) Observe(chunk []byte) bool {
	if o == nil || len(chunk) == 0 {
		return false
	}

	payloadObserved := false
	for _, b := range chunk {
		if o.swallowLineFeed {
			o.swallowLineFeed = false
			if b == '\n' {
				continue
			}
		}

		switch b {
		case '\r':
			if o.finishLine() {
				payloadObserved = true
			}
			o.swallowLineFeed = true
		case '\n':
			if o.finishLine() {
				payloadObserved = true
			}
		default:
			o.consumeLineByte(b)
		}
	}
	return payloadObserved
}

func (o *SSEPayloadObserver) consumeLineByte(b byte) {
	if o.linePrefixLen < len(o.linePrefix) {
		o.linePrefix[o.linePrefixLen] = b
		o.linePrefixLen++
	}

	// Byte position 5 is the first byte after "data:". Ignore spaces/tabs
	// there and later so "data:   \n\n" remains liveness-only, while JSON,
	// text deltas, and [DONE] count as provider payload.
	if o.lineLen >= len(o.linePrefix) && b != ' ' && b != '\t' {
		o.lineValueContent = true
	}
	o.lineLen++
}

func (o *SSEPayloadObserver) finishLine() bool {
	if o.lineLen == 0 {
		payload := o.eventHasPayload
		o.eventHasPayload = false
		o.resetLine()
		return payload
	}

	if o.linePrefixLen == len(o.linePrefix) &&
		o.linePrefix == [5]byte{'d', 'a', 't', 'a', ':'} &&
		o.lineValueContent {
		o.eventHasPayload = true
	}
	o.resetLine()
	return false
}

func (o *SSEPayloadObserver) resetLine() {
	o.linePrefixLen = 0
	o.lineLen = 0
	o.lineValueContent = false
}
