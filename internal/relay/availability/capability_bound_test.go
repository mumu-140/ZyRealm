package availability

import (
	"fmt"
	"testing"
	"time"
)

func TestCapabilityNegativeCacheHasHardBound(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	for i := 0; i < capabilityMaxEntries+128; i++ {
		RecordCapabilityNegative(91, "model-a", fmt.Sprintf("sig-%d", i), "cfg", "unsupported", now.Add(time.Duration(i)*time.Millisecond))
	}

	capabilityShared.mu.Lock()
	got := len(capabilityShared.entries)
	capabilityShared.mu.Unlock()
	if got > capabilityMaxEntries {
		t.Fatalf("capability cache size=%d, hard max=%d", got, capabilityMaxEntries)
	}
	if info := CapabilityInfo(91, "model-a", fmt.Sprintf("sig-%d", capabilityMaxEntries+127), "cfg", now.Add(time.Minute)); !info.Blocked {
		t.Fatalf("newest capability evidence should remain cached after bounded eviction")
	}
}
