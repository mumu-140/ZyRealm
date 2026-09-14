package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/protocolroute"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/gin-gonic/gin"
)

func TestFailoverStopReasonOuterCancellationBeforeNextProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayTestDB(t)
	availability.Reset()
	balancer.Reset()
	defer availability.Reset()
	defer balancer.Reset()

	const (
		providerA = 1901
		providerB = 1902
		keyA      = 1911
		modelName = "outer-cancel-stop-reason-model"
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

	outerCtx, cancelOuter := context.WithCancel(context.Background())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(outerCtx)
	request := &relayRequest{c: c, iter: iterator, attemptBudget: newRelayAttemptBudget()}
	channel := &dbmodel.Channel{ID: providerA, Name: "provider-a"}
	key := dbmodel.ChannelKey{ID: keyA}
	plan := protocolroute.NewAttemptPlan(protocolroute.PlanSpec{
		ChannelID: providerA, ChannelKeyID: keyA, RequestedModel: "public-model", UpstreamModel: modelName,
	})
	result := tracedFirstTokenTimeoutResult(t, c, request, iterator, channel, key, plan)
	h := &relayHandler{
		c: c, group: group, iterator: iterator, request: request,
		metrics: NewRelayMetrics(0, "public-model", nil, nil), heartbeat: &earlyHeartbeat{},
	}

	if terminal := h.handleAttemptResult(channel, key, plan, result); terminal {
		t.Fatalf("first-token timeout with an eligible alternative should still request failover")
	}
	cancelOuter()
	h.run()

	attempts := iterator.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("attempt count = %d, want 1", len(attempts))
	}
	if got := attemptFailoverStopReason(t, attempts[0]); got != "client_canceled" {
		t.Fatalf("failover stop reason = %q, want %q", got, "client_canceled")
	}
}
