package availability

import (
	"testing"
	"time"
)

func TestExplicitRetryAfterControlsRuntimeCooldown(t *testing.T) {
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)

	t.Run("model explicit overrides ladder", func(t *testing.T) {
		Reset()
		until := RecordModelFailureWithRetryAfter(10, "model-a", "model_capacity", base, 3*time.Second)
		if want := base.Add(3 * time.Second); !until.Equal(want) {
			t.Fatalf("cooldown until = %v, want %v", until, want)
		}
	})

	t.Run("model zero falls back to ladder", func(t *testing.T) {
		Reset()
		until := RecordModelFailureWithRetryAfter(11, "model-a", "model_capacity", base, 0)
		if want := base.Add(15 * time.Second); !until.Equal(want) {
			t.Fatalf("cooldown until = %v, want %v", until, want)
		}
	})

	t.Run("provider explicit overrides ladder", func(t *testing.T) {
		Reset()
		until := RecordProviderFailureWithRetryAfter(12, "provider_transient", base, 20*time.Second)
		if want := base.Add(20 * time.Second); !until.Equal(want) {
			t.Fatalf("cooldown until = %v, want %v", until, want)
		}
	})

	t.Run("explicit hint is capped", func(t *testing.T) {
		Reset()
		until := RecordModelFailureWithRetryAfter(13, "model-a", "model_capacity", base, 2*time.Hour)
		if want := base.Add(maxExplicitCooldown); !until.Equal(want) {
			t.Fatalf("cooldown until = %v, want cap %v", until, want)
		}
	})
}

func TestEnsureModelFailureRefinesActiveCooldownWithoutDoubleCharge(t *testing.T) {
	Reset()
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)

	first := RecordModelFailure(20, "model-a", "model_capacity", base)
	if want := base.Add(15 * time.Second); !first.Equal(want) {
		t.Fatalf("initial cooldown = %v, want %v", first, want)
	}

	refinedAt := base.Add(time.Millisecond)
	refined := EnsureModelFailureWithRetryAfter(20, "model-a", "model_capacity", refinedAt, 4*time.Second)
	if want := refinedAt.Add(4 * time.Second); !refined.Equal(want) {
		t.Fatalf("refined cooldown = %v, want %v", refined, want)
	}

	// If the refinement had double-charged this same attempt, the next real
	// failure would jump to the third model stage (5m) instead of the second (60s).
	nextFailureAt := refined.Add(time.Millisecond)
	next := EnsureModelFailure(20, "model-a", "model_capacity", nextFailureAt)
	if want := nextFailureAt.Add(60 * time.Second); !next.Equal(want) {
		t.Fatalf("next cooldown = %v, want second ladder stage %v", next, want)
	}
}
