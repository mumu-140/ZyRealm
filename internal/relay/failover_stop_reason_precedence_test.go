package relay

import (
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/balancer"
)

func TestFailoverStopReasonPreservesFirstConcreteReason(t *testing.T) {
	group := dbmodel.Group{
		Mode: dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{
			{ChannelID: 1801, ModelName: "stop-reason-model", Priority: 1, Weight: 1},
		},
	}
	iterator := balancer.NewIterator(group, 0, "public-model")
	if !iterator.Next() {
		t.Fatalf("expected one candidate")
	}
	span := iterator.StartAttempt(1801, 1811, "provider-1801")
	result := attemptResult{traceSpan: span}
	markFailoverStop(result, failoverStopNoAlternative)
	span.End(dbmodel.AttemptFailed, 0, "first token timeout")

	// The request-level exhaustion finalizer is more generic and must not erase
	// the earlier gate that precisely explains why failover could not advance.
	markFailoverStop(result, failoverStopCandidateExhausted)

	attempts := iterator.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("attempt count = %d, want 1", len(attempts))
	}
	if got := attemptFailoverStopReason(t, attempts[0]); got != "no_alternative" {
		t.Fatalf("failover stop reason = %q, want %q", got, "no_alternative")
	}
}
