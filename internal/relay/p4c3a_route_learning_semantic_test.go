package relay

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestShouldLearnManagedRouteConsumesExplicitRouteLearningPolicy(t *testing.T) {
	blocked := RoutingDecision{
		Valid:                 true,
		RouteLearningEligible: false,
		CircuitEffect:         "record_failure",
	}
	if shouldLearnManagedRoute(blocked, false, http.StatusInternalServerError) {
		t.Fatal("route learning must follow RouteLearningEligible=false, not legacy CircuitEffect")
	}

	allowed := RoutingDecision{
		Valid:                 true,
		RouteLearningEligible: true,
		CircuitEffect:         "none",
	}
	if !shouldLearnManagedRoute(allowed, false, http.StatusInternalServerError) {
		t.Fatal("route learning must follow RouteLearningEligible=true, not legacy CircuitEffect")
	}
}

func TestRoutingDecisionRouteLearningPolicyMatchesLegacyCircuitSemantics(t *testing.T) {
	tests := []struct {
		name   string
		ctx    context.Context
		result attemptResult
	}{
		{
			name: "generic 500 remains eligible",
			ctx:  context.Background(),
			result: attemptResult{
				Err:        errors.New("channel failed"),
				StatusCode: http.StatusInternalServerError,
			},
		},
		{
			name: "success remains ineligible",
			ctx:  context.Background(),
			result: attemptResult{
				Success:    true,
				StatusCode: http.StatusOK,
			},
		},
		{
			name: "provider transient remains eligible",
			ctx:  context.Background(),
			result: attemptResult{
				Err:        errors.New("upstream service temporarily unavailable"),
				StatusCode: http.StatusServiceUnavailable,
			},
		},
		{
			name: "model capability remains ineligible",
			ctx:  context.Background(),
			result: attemptResult{
				Err:               errors.New("channel failed"),
				StatusCode:        http.StatusUnauthorized,
				UpstreamErrorBody: `{"error":{"message":"reasoning effort xhigh is not supported","code":"invalid_api_key"}}`,
			},
		},
		{
			name: "credential failure remains ineligible",
			ctx:  context.Background(),
			result: attemptResult{
				Err:               errors.New("channel failed"),
				StatusCode:        http.StatusTooManyRequests,
				UpstreamErrorBody: `{"error":{"message":"account concurrency limit exceeded"}}`,
			},
		},
		{
			name: "model capacity remains eligible before retry status gate",
			ctx:  context.Background(),
			result: attemptResult{
				Err:               errors.New("channel failed"),
				StatusCode:        http.StatusTooManyRequests,
				UpstreamErrorBody: `{"error":{"message":"rate limit exceeded"}}`,
			},
		},
		{
			name: "content policy remains ineligible",
			ctx:  context.Background(),
			result: attemptResult{
				Err:               errors.New("channel failed"),
				StatusCode:        http.StatusInternalServerError,
				UpstreamErrorBody: `{"error":{"type":"content_policy_violation"}}`,
			},
		},
		{
			name: "ambiguous transport cancel remains ineligible",
			ctx:  context.Background(),
			result: attemptResult{
				Err:           context.Canceled,
				DispatchState: dispatchMaybeSent,
			},
		},
		{
			name: "first token timeout remains eligible before commitment",
			ctx:  context.Background(),
			result: attemptResult{
				Err:               errors.New("first token timeout"),
				FirstTokenTimeout: true,
				DispatchState:     dispatchMaybeSent,
			},
		},
		{
			name: "committed first token timeout remains ineligible",
			ctx:  context.Background(),
			result: attemptResult{
				Err:               errors.New("first token timeout"),
				FirstTokenTimeout: true,
				Written:           true,
				DispatchState:     dispatchMaybeSent,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := decideRoutingAttempt(tt.ctx, nil, 1, tt.result)
			want := decision.CircuitEffect != "none" && decision.CircuitEffect != "success"
			if decision.RouteLearningEligible != want {
				t.Fatalf("route learning eligible=%t, want %t from legacy circuit effect %q; decision=%+v",
					decision.RouteLearningEligible, want, decision.CircuitEffect, decision)
			}
		})
	}
}
