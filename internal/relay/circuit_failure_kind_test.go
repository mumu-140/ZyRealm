package relay

import (
	"testing"

	"github.com/bestruirui/octopus/internal/relay/balancer"
)

func TestCircuitFailureKindPolicy(t *testing.T) {
	tests := []struct {
		name         string
		retryEnabled bool
		statusCode   int
		want         balancer.FailureKind
	}{
		{name: "401 semantic auth envelope is circuit-neutral", statusCode: 401, want: balancer.FailureIgnore},
		{name: "403 semantic envelope stays circuit-neutral with retry enabled", retryEnabled: true, statusCode: 403, want: balancer.FailureIgnore},
		{name: "429 remains soft rate limit", retryEnabled: true, statusCode: 429, want: balancer.FailureSoftRateLimit},
		{name: "503 remains soft when retry is enabled", retryEnabled: true, statusCode: 503, want: balancer.FailureSoftRateLimit},
		{name: "500 remains hard provider failure", retryEnabled: true, statusCode: 500, want: balancer.FailureHard},
		{name: "transport failure remains hard", statusCode: 0, want: balancer.FailureHard},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := circuitFailureKind(tt.retryEnabled, tt.statusCode); got != tt.want {
				t.Fatalf("circuitFailureKind(retry=%t, status=%d) = %v, want %v", tt.retryEnabled, tt.statusCode, got, tt.want)
			}
		})
	}
}
