package availability

import (
	"testing"
	"time"
)

func TestProviderCooldownExpiresIntoSingleHalfOpenLease(t *testing.T) {
	Reset()
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	until := RecordProviderFailure(10, "upstream_503", base)
	if want := base.Add(5 * time.Second); !until.Equal(want) {
		t.Fatalf("provider cooldown until = %v, want %v", until, want)
	}
	if got := CandidateState(10, "model-a", base.Add(4*time.Second)); got != StateCooldown {
		t.Fatalf("state before expiry = %v, want cooldown", got)
	}
	if got := CandidateState(10, "model-a", base.Add(5*time.Second)); got != StateHalfOpen {
		t.Fatalf("state at expiry = %v, want half-open", got)
	}

	lease, ok := AcquireCandidate(10, "model-a", base.Add(5*time.Second))
	if !ok || len(lease.keys) != 1 {
		t.Fatalf("first half-open acquire = ok:%t keys:%d, want true/1", ok, len(lease.keys))
	}
	if _, ok := AcquireCandidate(10, "model-a", base.Add(5*time.Second)); ok {
		t.Fatalf("second concurrent half-open acquire should be rejected")
	}

	RecordSuccess(10, "model-a", base.Add(6*time.Second))
	if got := CandidateState(10, "model-a", base.Add(6*time.Second)); got != StateAvailable {
		t.Fatalf("state after successful half-open request = %v, want available", got)
	}
}

func TestModelCooldownDoesNotAffectOtherModels(t *testing.T) {
	Reset()
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	until := RecordModelFailure(20, "model-a", "first_token_timeout", base)
	if want := base.Add(15 * time.Second); !until.Equal(want) {
		t.Fatalf("model cooldown until = %v, want %v", until, want)
	}
	if got := CandidateState(20, "model-a", base); got != StateCooldown {
		t.Fatalf("failed model state = %v, want cooldown", got)
	}
	if got := CandidateState(20, "model-b", base); got != StateAvailable {
		t.Fatalf("unrelated model state = %v, want available", got)
	}
}

func TestRepeatedAmbiguousEvidenceEscalatesSuspectToCooldown(t *testing.T) {
	Reset()
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	if got := RecordModelSuspect(30, "model-a", "ambiguous_cancel", base); got != StateSuspect {
		t.Fatalf("first ambiguous failure state = %v, want suspect", got)
	}
	if got := CandidateState(30, "model-a", base.Add(time.Second)); got != StateSuspect {
		t.Fatalf("candidate after first ambiguous failure = %v, want suspect", got)
	}
	if got := RecordModelSuspect(30, "model-a", "ambiguous_cancel", base.Add(20*time.Second)); got != StateCooldown {
		t.Fatalf("second nearby ambiguous failure state = %v, want cooldown", got)
	}
	info := CandidateInfo(30, "model-a", base.Add(20*time.Second))
	if info.State != StateCooldown {
		t.Fatalf("candidate state after escalation = %v, want cooldown", info.State)
	}
	if want := base.Add(35 * time.Second); !info.CooldownUntil.Equal(want) {
		t.Fatalf("escalated cooldown until = %v, want %v", info.CooldownUntil, want)
	}
}

func TestAmbiguousEvidenceOutsideWindowRemainsSuspect(t *testing.T) {
	Reset()
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	RecordModelSuspect(31, "model-a", "ambiguous_cancel", base)
	if got := RecordModelSuspect(31, "model-a", "ambiguous_cancel", base.Add(31*time.Second)); got != StateSuspect {
		t.Fatalf("distant ambiguous failure state = %v, want suspect", got)
	}
}

func TestNeutralHalfOpenReleaseRemainsHalfOpen(t *testing.T) {
	Reset()
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	RecordModelFailure(40, "model-a", "first_token_timeout", base)
	lease, ok := AcquireCandidate(40, "model-a", base.Add(15*time.Second))
	if !ok || len(lease.keys) != 1 {
		t.Fatalf("half-open acquire = ok:%t keys:%d, want true/1", ok, len(lease.keys))
	}
	ReleaseLease(lease, base.Add(16*time.Second))
	if got := CandidateState(40, "model-a", base.Add(16*time.Second)); got != StateHalfOpen {
		t.Fatalf("neutral half-open release state = %v, want half-open", got)
	}
	if _, ok := AcquireCandidate(40, "model-a", base.Add(16*time.Second)); !ok {
		t.Fatalf("released neutral half-open lease should be acquirable again")
	}
}

func TestFailureCooldownEscalation(t *testing.T) {
	Reset()
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	providerWants := []time.Duration{5 * time.Second, 15 * time.Second, 60 * time.Second, 5 * time.Minute}
	for i, want := range providerWants {
		at := base.Add(time.Duration(i) * time.Second)
		until := RecordProviderFailure(50, "provider_transient", at)
		if got := until.Sub(at); got != want {
			t.Fatalf("provider streak %d cooldown = %v, want %v", i+1, got, want)
		}
	}

	Reset()
	modelWants := []time.Duration{15 * time.Second, 60 * time.Second, 5 * time.Minute}
	for i, want := range modelWants {
		at := base.Add(time.Duration(i) * time.Second)
		until := RecordModelFailure(60, "model-a", "first_token_timeout", at)
		if got := until.Sub(at); got != want {
			t.Fatalf("model streak %d cooldown = %v, want %v", i+1, got, want)
		}
	}
}
