package relay

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
)

func TestRecordRuntimeAvailabilityEvidenceScopesFailures(t *testing.T) {
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)

	t.Run("first token timeout is model scoped", func(t *testing.T) {
		availability.Reset()
		recordRuntimeAvailabilityEvidence(context.Background(), 10, "model-a", attemptResult{
			FirstTokenTimeout: true,
			Err:               errFirstTokenTimeout,
		}, base)
		if got := availability.CandidateState(10, "model-a", base); got != availability.StateCooldown {
			t.Fatalf("model-a state = %v, want cooldown", got)
		}
		if got := availability.CandidateState(10, "model-b", base); got != availability.StateAvailable {
			t.Fatalf("model-b state = %v, want available", got)
		}
	})

	t.Run("hard transport failure is provider scoped", func(t *testing.T) {
		availability.Reset()
		recordRuntimeAvailabilityEvidence(context.Background(), 20, "model-a", attemptResult{
			StatusCode:        502,
			UpstreamErrorBody: `{"error":{"message":"Upstream request failed"}}`,
		}, base)
		if got := availability.CandidateState(20, "model-a", base); got != availability.StateCooldown {
			t.Fatalf("model-a state = %v, want provider cooldown", got)
		}
		if got := availability.CandidateState(20, "model-b", base); got != availability.StateCooldown {
			t.Fatalf("model-b state = %v, want shared provider cooldown", got)
		}
	})

	t.Run("ambiguous cancel becomes suspect then cooldown", func(t *testing.T) {
		availability.Reset()
		result := attemptResult{Err: fmt.Errorf("failed to send request: %w", context.Canceled)}
		recordRuntimeAvailabilityEvidence(context.Background(), 30, "model-a", result, base)
		if got := availability.CandidateState(30, "model-a", base); got != availability.StateSuspect {
			t.Fatalf("first ambiguous state = %v, want suspect", got)
		}
		recordRuntimeAvailabilityEvidence(context.Background(), 30, "model-a", result, base.Add(10*time.Second))
		if got := availability.CandidateState(30, "model-a", base.Add(10*time.Second)); got != availability.StateCooldown {
			t.Fatalf("second ambiguous state = %v, want cooldown", got)
		}
	})

	t.Run("true client cancellation does not affect runtime", func(t *testing.T) {
		availability.Reset()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		recordRuntimeAvailabilityEvidence(ctx, 40, "model-a", attemptResult{
			Canceled: true,
			Err:      context.Canceled,
		}, base)
		if got := availability.CandidateState(40, "model-a", base); got != availability.StateAvailable {
			t.Fatalf("client cancellation state = %v, want available", got)
		}
	})
}

func TestRecordRuntimeAvailabilityEvidenceSuccessClearsRuntimeFailures(t *testing.T) {
	availability.Reset()
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	availability.RecordProviderFailure(50, "provider_transient", base)
	availability.RecordModelFailure(50, "model-a", "first_token_timeout", base)

	recordRuntimeAvailabilityEvidence(context.Background(), 50, "model-a", attemptResult{Success: true}, base.Add(time.Second))
	if got := availability.CandidateState(50, "model-a", base.Add(time.Second)); got != availability.StateAvailable {
		t.Fatalf("state after success = %v, want available", got)
	}
}
