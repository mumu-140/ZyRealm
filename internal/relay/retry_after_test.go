package relay

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfterAt(t *testing.T) {
	base := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{name: "delta seconds", header: "17", want: 17 * time.Second},
		{name: "http date", header: base.Add(42 * time.Second).Format(http.TimeFormat), want: 42 * time.Second},
		{name: "trimmed", header: " 9 ", want: 9 * time.Second},
		{name: "zero ignored", header: "0", want: 0},
		{name: "negative ignored", header: "-2", want: 0},
		{name: "past date ignored", header: base.Add(-time.Second).Format(http.TimeFormat), want: 0},
		{name: "invalid ignored", header: "later", want: 0},
		{name: "large delta capped", header: "86400", want: maxRuntimeRetryAfter},
		{name: "far future date capped", header: base.Add(24 * time.Hour).Format(http.TimeFormat), want: maxRuntimeRetryAfter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfterAt(tt.header, base); got != tt.want {
				t.Fatalf("parseRetryAfterAt(%q) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}

func TestComputeBackoffCapsLongRetryAfterPerRequest(t *testing.T) {
	if got := computeBackoff(1, 5*time.Minute); got != maxSameChannelRetryAfter {
		t.Fatalf("computeBackoff long Retry-After = %v, want %v", got, maxSameChannelRetryAfter)
	}
	if got := computeBackoff(1, 7*time.Second); got != 7*time.Second {
		t.Fatalf("computeBackoff short Retry-After = %v, want 7s", got)
	}
}
