package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/stream"
)

type committedStreamFailureSource struct {
	reads int
	err   error
}

func (s *committedStreamFailureSource) ReadEvent(context.Context) ([]byte, error) {
	s.reads++
	if s.reads == 1 {
		return []byte("data: partial\n\n"), nil
	}
	return nil, s.err
}

func (s *committedStreamFailureSource) Close() error { return nil }

type committedStreamFailureWriter struct {
	header  http.Header
	written bool
}

func newCommittedStreamFailureWriter() *committedStreamFailureWriter {
	return &committedStreamFailureWriter{header: make(http.Header)}
}

func (w *committedStreamFailureWriter) Write(data []byte) (int, error) {
	w.written = true
	return len(data), nil
}

func (w *committedStreamFailureWriter) Flush()                  {}
func (w *committedStreamFailureWriter) Written() bool           { return w.written }
func (w *committedStreamFailureWriter) Header() http.Header     { return w.header }
func (w *committedStreamFailureWriter) WriteHeader(code int)    {}

func committedStreamReadFailureResult(t *testing.T) attemptResult {
	t.Helper()
	upstreamErr := errors.New("stream error: stream ID 17; INTERNAL_ERROR; received from peer")
	writer := newCommittedStreamFailureWriter()
	processor := stream.NewStreamProcessor(stream.StreamConfig{
		Source:  &committedStreamFailureSource{err: upstreamErr},
		Writer:  writer,
		Context: context.Background(),
	})
	streamErr := processor.Run()
	if streamErr == nil {
		t.Fatal("expected upstream stream read failure")
	}
	if !writer.Written() || !processor.PayloadWritten() {
		t.Fatal("expected a semantic payload to be committed before the upstream stream failed")
	}
	return attemptResult{
		Written:         true,
		Err:             fmt.Errorf("channel committed-stream failed: %w", streamErr),
		StatusCode:      http.StatusOK,
		UpstreamStatus:  http.StatusOK,
		UpstreamStarted: true,
		DispatchState:   dispatchMaybeSent,
	}
}

func TestCommittedStreamReadFailureStopsReplayButCarriesModelHealthEvidence(t *testing.T) {
	result := committedStreamReadFailureResult(t)
	decision := decideRoutingAttempt(context.Background(), nil, 195, result)

	if !decision.Terminal || decision.Directive != routingDirectiveTerminal {
		t.Fatalf("committed stream decision must terminate current request: %+v", decision)
	}
	if decision.ReplaySafety != routingReplayCommitted {
		t.Fatalf("replay safety = %q, want downstream_committed", decision.ReplaySafety)
	}
	if decision.RetrySameCredential || decision.SkipProvider {
		t.Fatalf("committed stream must not trigger current-request retry/failover: %+v", decision)
	}
	if decision.RuleID != "committed_stream_failure" {
		t.Fatalf("rule = %q, want committed_stream_failure", decision.RuleID)
	}
	if decision.FailureScope != routingScopeProviderModel {
		t.Fatalf("failure scope = %q, want provider_model", decision.FailureScope)
	}
	if decision.RuntimeEffect != routingRuntimeModelCooldown {
		t.Fatalf("runtime effect = %q, want model_cooldown", decision.RuntimeEffect)
	}
	if decision.OutlierScope != scopeModel {
		t.Fatalf("outlier scope = %v, want model", decision.OutlierScope)
	}
	if !decision.RouteLearningCandidate {
		t.Fatal("committed upstream stream failure must remain managed-route learning evidence")
	}
}

func TestCommittedStreamReadFailureCoolsOnlyAffectedModel(t *testing.T) {
	availability.Reset()
	t.Cleanup(availability.Reset)
	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	result := committedStreamReadFailureResult(t)
	result.Decision = decideRoutingAttempt(context.Background(), nil, 195, result)

	recordRuntimeAvailabilityEvidence(context.Background(), 195, "glm-5.3-flash", result, base)

	if got := availability.CandidateState(195, "glm-5.3-flash", base); got != availability.StateCooldown {
		t.Fatalf("affected model state = %v, want cooldown", got)
	}
	if got := availability.CandidateState(195, "other-model", base); got != availability.StateAvailable {
		t.Fatalf("other model state = %v, want available", got)
	}
}

func TestCommittedNonStreamFailureRemainsRuntimeNeutral(t *testing.T) {
	result := attemptResult{
		Written:         true,
		Err:             errors.New("transform error: invalid downstream encoding"),
		StatusCode:      http.StatusOK,
		UpstreamStatus:  http.StatusOK,
		UpstreamStarted: true,
		DispatchState:   dispatchMaybeSent,
	}
	decision := decideRoutingAttempt(context.Background(), nil, 195, result)

	if !decision.Terminal || decision.ReplaySafety != routingReplayCommitted {
		t.Fatalf("committed non-stream failure must still stop replay: %+v", decision)
	}
	if decision.RuntimeEffect != routingRuntimeNone || decision.RouteLearningCandidate {
		t.Fatalf("local committed failure must stay runtime and route-learning neutral: %+v", decision)
	}
}
