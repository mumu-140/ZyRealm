package relay

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/relay/availability"
)

func TestRecordRuntimeAvailabilityEvidenceUsesRetryAfter(t *testing.T) {
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)

	t.Run("model capacity", func(t *testing.T) {
		availability.Reset()
		recordRuntimeAvailabilityEvidence(context.Background(), 60, "model-a", attemptResult{
			StatusCode:        http.StatusTooManyRequests,
			UpstreamStatus:    http.StatusTooManyRequests,
			UpstreamErrorBody: `{"error":{"message":"rate limit exceeded"}}`,
			RetryAfter:        4 * time.Second,
		}, base)
		info := availability.CandidateInfo(60, "model-a", base)
		if want := base.Add(4 * time.Second); !info.CooldownUntil.Equal(want) {
			t.Fatalf("model cooldown = %v, want %v", info.CooldownUntil, want)
		}
	})

	t.Run("provider transient", func(t *testing.T) {
		availability.Reset()
		recordRuntimeAvailabilityEvidence(context.Background(), 61, "model-a", attemptResult{
			StatusCode:        http.StatusServiceUnavailable,
			UpstreamStatus:    http.StatusServiceUnavailable,
			RetryAfter:        7 * time.Second,
			UpstreamErrorBody: `{"error":{"message":"upstream request failed"}}`,
		}, base)
		infoA := availability.CandidateInfo(61, "model-a", base)
		infoB := availability.CandidateInfo(61, "model-b", base)
		want := base.Add(7 * time.Second)
		if !infoA.CooldownUntil.Equal(want) || !infoB.CooldownUntil.Equal(want) {
			t.Fatalf("provider cooldowns = (%v, %v), want shared %v", infoA.CooldownUntil, infoB.CooldownUntil, want)
		}
	})
}
