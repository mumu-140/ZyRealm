package relay

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRelayControlInterruptCancelsChildOnly(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	control := newRelayControl(parent, LiveRequestSnapshot{
		RequestID: "lr_test_interrupt",
		Phase:     string(livePhaseRouting),
	})

	if !control.Interrupt() {
		t.Fatal("first interrupt should be accepted")
	}
	<-control.Context().Done()
	if cause := context.Cause(control.Context()); cause == nil || cause.Error() != "manual relay interrupt" {
		t.Fatalf("unexpected interrupt cause: %v", cause)
	}
	if parent.Err() != nil {
		t.Fatalf("manual interrupt canceled parent context: %v", parent.Err())
	}
	if control.Interrupt() {
		t.Fatal("second control interrupt should be a no-op")
	}
}

func TestNewRelayControlGeneratesIndependentLiveRequestID(t *testing.T) {
	first := newRelayControl(context.Background(), LiveRequestSnapshot{})
	second := newRelayControl(context.Background(), LiveRequestSnapshot{})

	firstID := first.Snapshot().RequestID
	secondID := second.Snapshot().RequestID
	if !strings.HasPrefix(firstID, "lr_") || !strings.HasPrefix(secondID, "lr_") {
		t.Fatalf("generated ids must use lr_ prefix: %q %q", firstID, secondID)
	}
	if firstID == secondID {
		t.Fatalf("generated ids must be unique: %q", firstID)
	}
}

func TestLiveRequestRegistrySnapshotsAreCopiedSortedAndInterruptible(t *testing.T) {
	older := newRelayControl(context.Background(), LiveRequestSnapshot{
		RequestID:      "lr_registry_old",
		StartedAt:      10,
		RequestedModel: "model-old",
	})
	newer := newRelayControl(context.Background(), LiveRequestSnapshot{
		RequestID:      "lr_registry_new",
		StartedAt:      20,
		RequestedModel: "model-new",
	})
	registerLiveRequest(older)
	registerLiveRequest(newer)
	t.Cleanup(func() {
		unregisterLiveRequest("lr_registry_old")
		unregisterLiveRequest("lr_registry_new")
	})

	got := ListLiveRequests()
	var filtered []LiveRequestSnapshot
	for _, item := range got {
		if item.RequestID == "lr_registry_old" || item.RequestID == "lr_registry_new" {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) != 2 {
		t.Fatalf("expected two registered snapshots, got %#v", filtered)
	}
	if filtered[0].RequestID != "lr_registry_new" || filtered[1].RequestID != "lr_registry_old" {
		t.Fatalf("snapshots not sorted newest-first: %#v", filtered)
	}

	filtered[0].RequestedModel = "mutated"
	for _, item := range ListLiveRequests() {
		if item.RequestID == "lr_registry_new" && item.RequestedModel != "model-new" {
			t.Fatalf("snapshot mutation leaked into registry: %#v", item)
		}
	}

	if !InterruptLiveRequest("lr_registry_new") {
		t.Fatal("registered id should be interruptible")
	}
	if !InterruptLiveRequest("lr_registry_new") {
		t.Fatal("interrupt API should remain idempotently successful while id is registered")
	}
	if !errors.Is(context.Cause(newer.Context()), errManualInterrupt) {
		t.Fatalf("registry interrupt cause = %v", context.Cause(newer.Context()))
	}
	if snapshot := newer.Snapshot(); !snapshot.InterruptRequested || snapshot.Phase != string(livePhaseInterrupting) {
		t.Fatalf("interrupt snapshot = %#v", snapshot)
	}

	unregisterLiveRequest("lr_registry_new")
	unregisterLiveRequest("lr_registry_new")
	if InterruptLiveRequest("lr_registry_new") {
		t.Fatal("unregistered id must not be interruptible")
	}
}

func TestInterruptLiveRequestUnknownID(t *testing.T) {
	if InterruptLiveRequest("lr_missing_request") {
		t.Fatal("unknown id should be rejected")
	}
}
