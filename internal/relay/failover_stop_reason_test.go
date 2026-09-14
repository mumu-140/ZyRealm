package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/protocolroute"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/gin-gonic/gin"
)

func TestFailoverStopReasonUnknownReplayBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayTestDB(t)
	availability.Reset()
	balancer.Reset()
	defer availability.Reset()
	defer balancer.Reset()

	const (
		providerA = 1101
		providerB = 1102
		keyA      = 1111
		modelName = "stop-reason-model"
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
		t.Fatalf("expected provider A to be the first candidate")
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	budget := newRelayAttemptBudget()
	budget.unknownReplayCount = defaultMaxUnknownCrossProviderReplay
	request := &relayRequest{c: c, iter: iterator, attemptBudget: budget}
	h := &relayHandler{
		c:         c,
		group:     group,
		iterator:  iterator,
		request:   request,
		metrics:   NewRelayMetrics(0, "public-model", nil, nil),
		heartbeat: &earlyHeartbeat{},
	}
	channel := &dbmodel.Channel{ID: providerA, Name: "provider-a"}
	key := dbmodel.ChannelKey{ID: keyA}
	plan := protocolroute.NewAttemptPlan(protocolroute.PlanSpec{
		ChannelID:      providerA,
		ChannelKeyID:   keyA,
		RequestedModel: "public-model",
		UpstreamModel:  modelName,
	})
	result := tracedFirstTokenTimeoutResult(t, c, request, iterator, channel, key, plan)

	if terminal := h.handleAttemptResult(channel, key, plan, result); !terminal {
		t.Fatalf("expected exhausted unknown-outcome replay budget to terminate")
	}
	attempts := iterator.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("attempt count = %d, want 1", len(attempts))
	}
	if got := attemptFailoverStopReason(t, attempts[0]); got != "unknown_replay_budget" {
		t.Fatalf("failover stop reason = %q, want %q", got, "unknown_replay_budget")
	}
}

func TestFailoverStopReasonNoAlternative(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	balancer.Reset()
	defer availability.Reset()
	defer balancer.Reset()

	const (
		providerA = 1201
		keyA      = 1211
		modelName = "stop-reason-model"
	)
	group := dbmodel.Group{
		Mode: dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{
			{ChannelID: providerA, ModelName: modelName, Priority: 1, Weight: 1},
		},
	}
	iterator := balancer.NewIterator(group, 0, "public-model")
	if !iterator.Next() || iterator.Item().ChannelID != providerA {
		t.Fatalf("expected provider A to be the first candidate")
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request := &relayRequest{c: c, iter: iterator, attemptBudget: newRelayAttemptBudget()}
	h := &relayHandler{c: c, group: group, iterator: iterator, request: request}
	channel := &dbmodel.Channel{ID: providerA, Name: "provider-a"}
	key := dbmodel.ChannelKey{ID: keyA}
	plan := protocolroute.NewAttemptPlan(protocolroute.PlanSpec{
		ChannelID:      providerA,
		ChannelKeyID:   keyA,
		RequestedModel: "public-model",
		UpstreamModel:  modelName,
	})
	result := tracedFirstTokenTimeoutResult(t, c, request, iterator, channel, key, plan)

	if terminal := h.handleAttemptResult(channel, key, plan, result); terminal {
		t.Fatalf("no-alternative exhaustion is finalized by the outer candidate loop, not inside the attempt handler")
	}
	attempts := iterator.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("attempt count = %d, want 1", len(attempts))
	}
	if got := attemptFailoverStopReason(t, attempts[0]); got != "no_alternative" {
		t.Fatalf("failover stop reason = %q, want %q", got, "no_alternative")
	}
}

func TestFailoverStopReasonDownstreamCommitted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	balancer.Reset()
	defer availability.Reset()
	defer balancer.Reset()

	c, iterator, request, channel, key, plan := newStopReasonTraceHarness(t, 1301, 1302, 1311)
	span := iterator.StartAttempt(channel.ID, key.ID, channel.Name)
	ra := &relayAttempt{relayRequest: request, channel: channel, usedKey: key, plan: plan}
	result := ra.attachRoutingDecision(span, attemptResult{
		FirstTokenTimeout: true,
		Written:           true,
		Err:               fmt.Errorf("channel %s failed after payload: %w", channel.Name, errFirstTokenTimeout),
		DispatchState:     dispatchMaybeSent,
	})
	span.End(dbmodel.AttemptFailed, 0, result.Err.Error())

	if got := attemptFailoverStopReason(t, iterator.Attempts()[0]); got != "downstream_committed" {
		t.Fatalf("failover stop reason = %q, want %q", got, "downstream_committed")
	}
	_ = c
}

func TestFailoverStopReasonClientCanceled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	balancer.Reset()
	defer availability.Reset()
	defer balancer.Reset()

	_, iterator, request, channel, key, plan := newStopReasonTraceHarness(t, 1401, 1402, 1411)
	span := iterator.StartAttempt(channel.ID, key.ID, channel.Name)
	ra := &relayAttempt{relayRequest: request, channel: channel, usedKey: key, plan: plan}
	result := ra.attachRoutingDecision(span, attemptResult{
		Canceled:      true,
		Err:           context.Canceled,
		DispatchState: dispatchMaybeSent,
	})
	span.End(dbmodel.AttemptFailed, 0, result.Err.Error())

	if got := attemptFailoverStopReason(t, iterator.Attempts()[0]); got != "client_canceled" {
		t.Fatalf("failover stop reason = %q, want %q", got, "client_canceled")
	}
}

func TestFailoverStopReasonWireAttemptBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayTestDB(t)
	availability.Reset()
	balancer.Reset()
	defer availability.Reset()
	defer balancer.Reset()

	c, iterator, request, channel, key, plan := newStopReasonTraceHarness(t, 1501, 1502, 1511)
	result := tracedFirstTokenTimeoutResult(t, c, request, iterator, channel, key, plan)
	iterator.SkipProvider(1501)
	iterator.SkipProvider(1502)
	h := &relayHandler{
		c: c, group: dbmodel.Group{Mode: dbmodel.GroupModeFailover}, iterator: iterator,
		request: request, metrics: NewRelayMetrics(0, "public-model", nil, nil), heartbeat: &earlyHeartbeat{},
		lastErr: errRelayWireAttemptsExceeded, lastResult: result,
	}
	h.run()

	if got := attemptFailoverStopReason(t, iterator.Attempts()[0]); got != "wire_attempt_budget" {
		t.Fatalf("failover stop reason = %q, want %q", got, "wire_attempt_budget")
	}
}

func TestFailoverStopReasonProviderAttemptBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayTestDB(t)
	availability.Reset()
	balancer.Reset()
	defer availability.Reset()
	defer balancer.Reset()

	c, iterator, request, channel, key, plan := newStopReasonTraceHarness(t, 1601, 1602, 1611)
	result := tracedFirstTokenTimeoutResult(t, c, request, iterator, channel, key, plan)
	iterator.SkipProvider(1601)
	iterator.SkipProvider(1602)
	h := &relayHandler{
		c: c, group: dbmodel.Group{Mode: dbmodel.GroupModeFailover}, iterator: iterator,
		request: request, metrics: NewRelayMetrics(0, "public-model", nil, nil), heartbeat: &earlyHeartbeat{},
		lastErr: errRelayProviderAttemptsExceeded, lastResult: result,
	}
	h.run()

	if got := attemptFailoverStopReason(t, iterator.Attempts()[0]); got != "provider_attempt_budget" {
		t.Fatalf("failover stop reason = %q, want %q", got, "provider_attempt_budget")
	}
}

func TestFailoverStopReasonCandidateExhausted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayTestDB(t)
	availability.Reset()
	balancer.Reset()
	defer availability.Reset()
	defer balancer.Reset()

	c, iterator, request, channel, key, plan := newStopReasonTraceHarness(t, 1701, 1702, 1711)
	result := tracedFirstTokenTimeoutResult(t, c, request, iterator, channel, key, plan)
	iterator.SkipProvider(1701)
	iterator.SkipProvider(1702)
	h := &relayHandler{
		c: c, group: dbmodel.Group{Mode: dbmodel.GroupModeFailover}, iterator: iterator,
		request: request, metrics: NewRelayMetrics(0, "public-model", nil, nil), heartbeat: &earlyHeartbeat{},
		lastErr: result.Err, lastResult: result,
	}
	h.run()

	if got := attemptFailoverStopReason(t, iterator.Attempts()[0]); got != "candidate_exhausted" {
		t.Fatalf("failover stop reason = %q, want %q", got, "candidate_exhausted")
	}
}

func newStopReasonTraceHarness(t *testing.T, providerA, providerB, keyA int) (*gin.Context, *balancer.Iterator, *relayRequest, *dbmodel.Channel, dbmodel.ChannelKey, *protocolroute.AttemptPlan) {
	t.Helper()
	const modelName = "stop-reason-model"
	group := dbmodel.Group{
		Mode: dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{
			{ChannelID: providerA, ModelName: modelName, Priority: 1, Weight: 1},
			{ChannelID: providerB, ModelName: modelName, Priority: 2, Weight: 1},
		},
	}
	iterator := balancer.NewIterator(group, 0, "public-model")
	if !iterator.Next() || iterator.Item().ChannelID != providerA {
		t.Fatalf("expected provider %d to be the first candidate", providerA)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request := &relayRequest{c: c, iter: iterator, attemptBudget: newRelayAttemptBudget()}
	channel := &dbmodel.Channel{ID: providerA, Name: fmt.Sprintf("provider-%d", providerA)}
	key := dbmodel.ChannelKey{ID: keyA}
	plan := protocolroute.NewAttemptPlan(protocolroute.PlanSpec{
		ChannelID: providerA, ChannelKeyID: keyA, RequestedModel: "public-model", UpstreamModel: modelName,
	})
	return c, iterator, request, channel, key, plan
}

func tracedFirstTokenTimeoutResult(
	t *testing.T,
	c *gin.Context,
	request *relayRequest,
	iterator *balancer.Iterator,
	channel *dbmodel.Channel,
	key dbmodel.ChannelKey,
	plan *protocolroute.AttemptPlan,
) attemptResult {
	t.Helper()
	span := iterator.StartAttempt(channel.ID, key.ID, channel.Name)
	ra := &relayAttempt{
		relayRequest: request,
		channel:      channel,
		usedKey:      key,
		plan:         plan,
	}
	result := ra.attachRoutingDecision(span, attemptResult{
		FirstTokenTimeout: true,
		Err:               fmt.Errorf("channel %s failed: %w (2s)", channel.Name, errFirstTokenTimeout),
		DispatchState:     dispatchMaybeSent,
	})
	span.End(dbmodel.AttemptFailed, 0, result.Err.Error())
	return result
}

func attemptFailoverStopReason(t *testing.T, attempt dbmodel.ChannelAttempt) string {
	t.Helper()
	raw, err := json.Marshal(attempt)
	if err != nil {
		t.Fatalf("marshal attempt: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal attempt: %v", err)
	}
	value, _ := fields["failover_stop_reason"].(string)
	return value
}
