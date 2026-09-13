package relay

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/protocolroute"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/gin-gonic/gin"
)

func TestHandleAttemptResultAmbiguousCancellationSkipsProviderOnceWithoutHardPenalty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	balancer.Reset()
	defer availability.Reset()
	defer balancer.Reset()

	const (
		providerA = 101
		providerB = 202
		keyA      = 1001
		modelName = "upstream-model"
	)
	group := dbmodel.Group{
		Mode: dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{
			{ChannelID: providerA, ModelName: modelName, Priority: 1, Weight: 1},
			{ChannelID: providerB, ModelName: modelName, Priority: 2, Weight: 1},
		},
	}
	iterator := balancer.NewIterator(group, 0, "public-model")
	if !iterator.Next() || iterator.Item().ChannelID != providerA {
		t.Fatalf("expected provider A to be first candidate")
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	budget := newRelayAttemptBudget()
	h := &relayHandler{
		c:        c,
		group:    group,
		iterator: iterator,
		request:  &relayRequest{attemptBudget: budget},
	}
	plan := protocolroute.NewAttemptPlan(protocolroute.PlanSpec{
		ChannelID:      providerA,
		ChannelKeyID:   keyA,
		RequestedModel: "public-model",
		UpstreamModel:  modelName,
	})
	result := attemptResult{
		Err:           fmt.Errorf("channel provider-a failed: failed to send request: %w", context.Canceled),
		DispatchState: dispatchMaybeSent,
	}

	if terminal := h.handleAttemptResult(&dbmodel.Channel{ID: providerA, Name: "provider-a"}, dbmodel.ChannelKey{ID: keyA}, plan, result); terminal {
		t.Fatalf("expected ambiguous transport cancellation with live outer context to remain failover-eligible")
	}
	if budget.unknownReplayCount != 1 {
		t.Fatalf("unknown cross-provider replay count = %d, want 1", budget.unknownReplayCount)
	}
	if budget.tryUnknownCrossProviderReplay() {
		t.Fatalf("expected balanced replay budget to allow at most one unknown cross-provider replay")
	}
	if state := availability.CandidateState(providerA, modelName, time.Now()); state != availability.StateSuspect {
		t.Fatalf("provider A runtime state = %v, want soft suspect evidence", state)
	}
	if tripped, _ := balancer.IsTripped(providerA, keyA, modelName); tripped {
		t.Fatalf("ambiguous transport cancellation must not trip the hard key/model circuit")
	}
	if !iterator.Next() {
		t.Fatalf("expected provider B to remain available after request-local skip of provider A")
	}
	if got := iterator.Item().ChannelID; got != providerB {
		t.Fatalf("next provider after ambiguous cancellation = %d, want %d", got, providerB)
	}
}

func TestHandleAttemptResultNotSentCancellationDoesNotConsumeUnknownReplayBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	balancer.Reset()
	defer availability.Reset()
	defer balancer.Reset()

	const (
		providerA = 301
		providerB = 302
		keyA      = 3001
		modelName = "upstream-model"
	)
	group := dbmodel.Group{
		Mode: dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{
			{ChannelID: providerA, ModelName: modelName, Priority: 1, Weight: 1},
			{ChannelID: providerB, ModelName: modelName, Priority: 2, Weight: 1},
		},
	}
	iterator := balancer.NewIterator(group, 0, "public-model")
	if !iterator.Next() || iterator.Item().ChannelID != providerA {
		t.Fatalf("expected provider A to be first candidate")
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	budget := newRelayAttemptBudget()
	h := &relayHandler{
		c:        c,
		group:    group,
		iterator: iterator,
		request:  &relayRequest{attemptBudget: budget},
	}
	plan := protocolroute.NewAttemptPlan(protocolroute.PlanSpec{
		ChannelID:      providerA,
		ChannelKeyID:   keyA,
		RequestedModel: "public-model",
		UpstreamModel:  modelName,
	})
	result := attemptResult{
		Err:           fmt.Errorf("channel provider-a failed before transport send: %w", context.Canceled),
		DispatchState: dispatchNotSent,
	}

	if terminal := h.handleAttemptResult(&dbmodel.Channel{ID: providerA, Name: "provider-a"}, dbmodel.ChannelKey{ID: keyA}, plan, result); terminal {
		t.Fatalf("NOT_SENT cancellation should remain safe to fail over")
	}
	if budget.unknownReplayCount != 0 {
		t.Fatalf("NOT_SENT cancellation consumed unknown replay budget: got %d, want 0", budget.unknownReplayCount)
	}
	if !iterator.Next() || iterator.Item().ChannelID != providerB {
		t.Fatalf("expected safe failover to provider B after NOT_SENT cancellation")
	}
}
