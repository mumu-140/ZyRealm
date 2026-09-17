package task

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/grouphealth"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/outlierwindow"
)

func TestRunOutlierRetire_NonUnavailableProbeDoesNotRetireChannel(t *testing.T) {
	tests := []struct {
		name    string
		outcome grouphealth.ProbeOutcome
		status  int
	}{
		{name: "credential rejected", outcome: grouphealth.ProbeOutcomeCredentialRejected, status: 401},
		{name: "provider rejected", outcome: grouphealth.ProbeOutcomeRejected, status: 403},
		{name: "rate limited", outcome: grouphealth.ProbeOutcomeRateLimited, status: 429},
		{name: "inconclusive", outcome: grouphealth.ProbeOutcomeInconclusive, status: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := setupOutlierTestDB(t)
			cfg := testOutlierConfig()
			outlierwindow.Configure(cfg.window)
			now := time.Now()
			base := now.Add(-time.Minute)

			siteID, acc := createSiteAccountFixture(t, ctx)
			bad := createProjectedChannel(t, ctx, siteID, acc, "bad-"+tt.name, true, true)
			good := createProjectedChannel(t, ctx, siteID, acc, "good-"+tt.name, true, true)
			reportN(bad, false, 12, base)
			reportN(good, true, 12, base)

			prober := &fakeProber{fallback: grouphealth.ProbeResult{
				Success:    false,
				Outcome:    tt.outcome,
				HTTPStatus: tt.status,
			}}
			runOutlierRetire(ctx, prober, cfg, now)

			if !mustChannelEnabled(t, ctx, bad) {
				t.Fatalf("channel %d disabled for non-unavailable probe outcome %q", bad, tt.outcome)
			}
			if _, err := op.SiteChannelOutlierGet(bad, ctx); err == nil {
				t.Fatalf("channel %d retired for non-unavailable probe outcome %q", bad, tt.outcome)
			}
		})
	}
}

func TestRunOutlierRetire_SiteOutageRejectedProbeDoesNotDisableSiblings(t *testing.T) {
	ctx := setupOutlierTestDB(t)
	cfg := testOutlierConfig()
	outlierwindow.Configure(cfg.window)
	now := time.Now()
	base := now.Add(-time.Minute)

	siteID, acc := createSiteAccountFixture(t, ctx)
	ids := []int{
		createProjectedChannel(t, ctx, siteID, acc, "reject-a", true, true),
		createProjectedChannel(t, ctx, siteID, acc, "reject-b", true, true),
	}
	for _, id := range ids {
		reportN(id, false, 12, base)
	}

	prober := &fakeProber{fallback: grouphealth.ProbeResult{
		Success:    false,
		Outcome:    grouphealth.ProbeOutcomeRejected,
		HTTPStatus: 403,
	}}
	runOutlierRetire(ctx, prober, cfg, now)

	for _, id := range ids {
		if !mustChannelEnabled(t, ctx, id) {
			t.Fatalf("site sibling %d disabled after rejected diagnostic probe", id)
		}
		if _, err := op.SiteChannelOutlierGet(id, ctx); err == nil {
			t.Fatalf("site sibling %d retired after rejected diagnostic probe", id)
		}
	}
}

func TestRunOutlierRetire_UnavailableProbeStillConfirmsRetirement(t *testing.T) {
	ctx := setupOutlierTestDB(t)
	cfg := testOutlierConfig()
	outlierwindow.Configure(cfg.window)
	now := time.Now()
	base := now.Add(-time.Minute)

	siteID, acc := createSiteAccountFixture(t, ctx)
	bad := createProjectedChannel(t, ctx, siteID, acc, "unavailable-bad", true, true)
	good := createProjectedChannel(t, ctx, siteID, acc, "unavailable-good", true, true)
	reportN(bad, false, 12, base)
	reportN(good, true, 12, base)

	prober := &fakeProber{fallback: grouphealth.ProbeResult{
		Success:      false,
		Outcome:      grouphealth.ProbeOutcomeUnavailable,
		HTTPStatus:   503,
		ErrorMessage: "upstream error: 503",
	}}
	runOutlierRetire(ctx, prober, cfg, now)

	if mustChannelEnabled(t, ctx, bad) {
		t.Fatalf("channel %d should retire after passive gates plus unavailable probe", bad)
	}
	if _, err := op.SiteChannelOutlierGet(bad, ctx); err != nil {
		t.Fatalf("expected retired state after unavailable probe: %v", err)
	}
}
