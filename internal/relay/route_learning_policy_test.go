package relay

import "testing"

func TestShouldLearnManagedRouteUsesExplicitRouteLearningCandidate(t *testing.T) {
	tests := []struct {
		name         string
		candidate    bool
		retryEnabled bool
		statusCode   int
		want         bool
	}{
		{
			name:       "hard provider 500 learns",
			candidate:  true,
			statusCode: 500,
			want:       true,
		},
		{
			name:       "transport failure learns",
			candidate:  true,
			statusCode: 0,
			want:       true,
		},
		{
			name:         "retry enabled 503 stays soft and does not learn",
			candidate:    true,
			retryEnabled: true,
			statusCode:   503,
			want:         false,
		},
		{
			name:       "retry disabled 503 remains hard and learns",
			candidate:  true,
			statusCode: 503,
			want:       true,
		},
		{
			name:         "retry enabled 429 stays soft and does not learn",
			candidate:    true,
			retryEnabled: true,
			statusCode:   429,
			want:         false,
		},
		{
			name:       "generic 401 stays semantic and does not learn",
			candidate:  true,
			statusCode: 401,
			want:       false,
		},
		{
			name:       "policy neutral decision does not learn",
			candidate:  false,
			statusCode: 500,
			want:       false,
		},
		{
			name:       "success decision does not learn",
			candidate:  false,
			statusCode: 200,
			want:       false,
		},
		{
			name:         "model capacity policy remains soft when retry is enabled",
			candidate:    true,
			retryEnabled: true,
			statusCode:   503,
			want:         false,
		},
		{
			name:       "committed stream health evidence preserves hard gate",
			candidate:  true,
			statusCode: 200,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldLearnManagedRoute(tt.candidate, tt.retryEnabled, tt.statusCode); got != tt.want {
				t.Fatalf("shouldLearnManagedRoute(candidate=%t, retry=%t, status=%d) = %t, want %t",
					tt.candidate, tt.retryEnabled, tt.statusCode, got, tt.want)
			}
		})
	}
}
