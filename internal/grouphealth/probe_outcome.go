package grouphealth

import (
	"context"
	"errors"
	"io"
	"net"
)

// ProbeOutcome classifies active synthetic-probe evidence. Only unavailable is
// strong enough to confirm provider/channel unavailability for POR after its
// existing passive and sibling gates have already fired.
type ProbeOutcome string

const (
	ProbeOutcomeSuccess            ProbeOutcome = "success"
	ProbeOutcomeUnavailable        ProbeOutcome = "unavailable"
	ProbeOutcomeCredentialRejected ProbeOutcome = "credential_rejected"
	ProbeOutcomeRateLimited        ProbeOutcome = "rate_limited"
	ProbeOutcomeRejected           ProbeOutcome = "rejected"
	ProbeOutcomeInconclusive       ProbeOutcome = "inconclusive"
)

func classifyProbeHTTP(status int) ProbeOutcome {
	switch {
	case status >= 200 && status < 300:
		return ProbeOutcomeSuccess
	case status == 401:
		return ProbeOutcomeCredentialRejected
	case status == 429:
		return ProbeOutcomeRateLimited
	case status == 408 || status >= 500:
		return ProbeOutcomeUnavailable
	case status >= 400 && status < 500:
		return ProbeOutcomeRejected
	default:
		return ProbeOutcomeInconclusive
	}
}

func classifyProbeTransport(parentErr, probeErr error) ProbeOutcome {
	if parentErr != nil {
		return ProbeOutcomeInconclusive
	}
	if probeErr == nil {
		return ProbeOutcomeInconclusive
	}
	if errors.Is(probeErr, context.DeadlineExceeded) || errors.Is(probeErr, io.EOF) || errors.Is(probeErr, io.ErrUnexpectedEOF) {
		return ProbeOutcomeUnavailable
	}
	var netErr net.Error
	if errors.As(probeErr, &netErr) {
		return ProbeOutcomeUnavailable
	}
	return ProbeOutcomeInconclusive
}
