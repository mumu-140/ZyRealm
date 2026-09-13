package availability

import (
	"testing"
	"time"
)

func TestCapabilityNegativeCacheLifecycle(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	base := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)

	expires := RecordCapabilityNegative(7, "model-a", "sig-a", "cfg-a", "reasoning effort unsupported", base)
	if want := base.Add(30 * time.Minute); !expires.Equal(want) {
		t.Fatalf("expires = %v, want %v", expires, want)
	}
	info := CapabilityInfo(7, "model-a", "sig-a", "cfg-a", base.Add(time.Minute))
	if !info.Blocked || info.Reason != "reasoning effort unsupported" || !info.ExpiresAt.Equal(expires) {
		t.Fatalf("unexpected active capability snapshot: %+v", info)
	}

	if got := CapabilityInfo(7, "model-a", "sig-b", "cfg-a", base.Add(time.Minute)); got.Blocked {
		t.Fatalf("different capability signature must remain eligible: %+v", got)
	}
	if got := CapabilityInfo(7, "model-b", "sig-a", "cfg-a", base.Add(time.Minute)); got.Blocked {
		t.Fatalf("different upstream model must remain eligible: %+v", got)
	}
	if got := CapabilityInfo(8, "model-a", "sig-a", "cfg-a", base.Add(time.Minute)); got.Blocked {
		t.Fatalf("different provider must remain eligible: %+v", got)
	}
}

func TestCapabilityNegativeCacheInvalidatesOnConfigChangeExpiryAndSuccess(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	base := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)

	t.Run("config change", func(t *testing.T) {
		Reset()
		RecordCapabilityNegative(9, "model-a", "sig-a", "cfg-old", "unsupported", base)
		if got := CapabilityInfo(9, "model-a", "sig-a", "cfg-new", base.Add(time.Minute)); got.Blocked {
			t.Fatalf("changed config fingerprint must bypass old negative entry: %+v", got)
		}
	})

	t.Run("ttl expiry", func(t *testing.T) {
		Reset()
		RecordCapabilityNegative(10, "model-a", "sig-a", "cfg", "unsupported", base)
		if got := CapabilityInfo(10, "model-a", "sig-a", "cfg", base.Add(capabilityNegativeTTL)); got.Blocked {
			t.Fatalf("entry must expire at ttl boundary: %+v", got)
		}
	})

	t.Run("matching success clears", func(t *testing.T) {
		Reset()
		RecordCapabilityNegative(11, "model-a", "sig-a", "cfg", "unsupported", base)
		ClearCapabilityNegative(11, "model-a", "sig-a", "cfg")
		if got := CapabilityInfo(11, "model-a", "sig-a", "cfg", base.Add(time.Minute)); got.Blocked {
			t.Fatalf("matching success must clear exact negative entry: %+v", got)
		}
	})

	t.Run("old config success cannot clear new evidence", func(t *testing.T) {
		Reset()
		RecordCapabilityNegative(12, "model-a", "sig-a", "cfg-new", "unsupported", base)
		ClearCapabilityNegative(12, "model-a", "sig-a", "cfg-old")
		if got := CapabilityInfo(12, "model-a", "sig-a", "cfg-new", base.Add(time.Minute)); !got.Blocked {
			t.Fatalf("stale success must not clear current-config negative entry")
		}
	})
}
