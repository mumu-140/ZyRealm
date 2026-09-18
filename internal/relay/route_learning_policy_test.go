package relay

import "testing"

func TestShouldLearnManagedRoutePreservesLegacyBreakerGate(t *testing.T) {
	tests := []struct {
		name         string
		decision     RoutingDecision
		retryEnabled bool
		statusCode   int
		want         bool
	}{
		{
			name:       "hard provider 500 learns",
			decision:   RoutingDecision{Valid: true, CircuitEffect: "record_failure"},
			statusCode: 500,
			want:       true,
		},
		{
			name:       "transport failure learns",
			decision:   RoutingDecision{Valid: true, CircuitEffect: "record_failure"},
			statusCode: 0,
			want:       true,
		},
		{
			name:         "retry enabled 503 stays soft and does not learn",
			decision:     RoutingDecision{Valid: true, CircuitEffect: "record_failure"},
			retryEnabled: true,
			statusCode:   503,
			want:         false,
		},
		{
			name:       "retry disabled 503 remains hard and learns",
			decision:   RoutingDecision{Valid: true, CircuitEffect: "record_failure"},
			statusCode: 503,
			want:       true,
		},
		{
			name:         "retry enabled 429 stays soft and does not learn",
			decision:     RoutingDecision{Valid: true, CircuitEffect: "record_failure"},
			retryEnabled: true,
			statusCode:   429,
			want:         false,
		},
		{
			name:       "generic 401 stays semantic and does not learn",
			decision:   RoutingDecision{Valid: true, CircuitEffect: "record_failure"},
			statusCode: 401,
			want:       false,
		},
		{
			name:       "policy neutral decision does not learn",
			decision:   RoutingDecision{Valid: true, CircuitEffect: "none"},
			statusCode: 500,
			want:       false,
		},
		{
			name:       "success decision does not learn",
			decision:   RoutingDecision{Valid: true, CircuitEffect: "success"},
			statusCode: 200,
			want:       false,
		},
		{
			name:         "model capacity policy remains soft when retry is enabled",
			decision:     RoutingDecision{Valid: true, CircuitEffect: "rate_limit_policy"},
			retryEnabled: true,
			statusCode:   503,
			want:         false,
		},
		{
			name:       "committed stream health evidence preserves hard gate",
			decision:   RoutingDecision{Valid: true, RuleID: "committed_stream_failure", CircuitEffect: "record_failure"},
			statusCode: 200,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldLearnManagedRoute(tt.decision, tt.retryEnabled, tt.statusCode); got != tt.want {
				t.Fatalf("shouldLearnManagedRoute(%+v, retry=%t, status=%d) = %t, want %t",
					tt.decision, tt.retryEnabled, tt.statusCode, got, tt.want)
			}
		})
	}
}
