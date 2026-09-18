package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/gin-gonic/gin"
)

func p5cSource(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve P5C source path")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func p5cWaitRelayLog(t *testing.T, ch <-chan model.RelayLog, requestModel string) model.RelayLog {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case item := <-ch:
			if item.RequestModelName == requestModel {
				return item
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for relay log for %q", requestModel)
		}
	}
}

func p5cFirstRealAttempt(t *testing.T, item model.RelayLog) model.ChannelAttempt {
	t.Helper()
	for _, attempt := range item.Attempts {
		if attempt.AttemptKind != "decision_only" &&
			(attempt.Status == model.AttemptSuccess || attempt.Status == model.AttemptFailed) {
			return attempt
		}
	}
	t.Fatalf("relay log for %q has no real attempt: %+v", item.RequestModelName, item.Attempts)
	return model.ChannelAttempt{}
}

func TestP5CImagesConsumesCoordinatorEffectsAndPreservesPolicy(t *testing.T) {
	source := p5cSource(t, "images.go")

	for _, required := range []string{
		"coordinateAttemptOutcome(result)",
		"applyRuntimeAvailabilityEffect(",
		"coordination.Effects.OutlierScope",
		"attachSidepathRoutingTrace(",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Images P5C migration missing %q", required)
		}
	}
	for _, retired := range []string{
		"recordRuntimeAvailabilityEvidence(",
		"decision.Domain == failureDomainCredential",
		"decision.OutlierScope",
	} {
		if strings.Contains(source, retired) {
			t.Fatalf("Images still consumes direct/compat effect via %q", retired)
		}
	}

	for _, invariant := range []string{
		"maxUpstreamStarts = group.MaxRetries + 1",
		"balancer.TryAcquireChannel(",
		"balancer.TryConsumeChannelRPM(",
		"resolveSidepathDirective(coordination.Disposition)",
	} {
		if !strings.Contains(source, invariant) {
			t.Fatalf("P5C changed Images retry/admission invariant %q", invariant)
		}
	}
}

func TestP5CCompactConsumesCoordinatorEffectsAndPreservesPolicy(t *testing.T) {
	source := p5cSource(t, "compact.go")

	for _, required := range []string{
		"coordinateAttemptOutcome(result)",
		"applyRuntimeAvailabilityEffect(",
		"coordination.Effects.OutlierScope",
		"attachSidepathRoutingTrace(",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Compact P5C migration missing %q", required)
		}
	}
	for _, retired := range []string{
		"recordRuntimeAvailabilityEvidence(",
		"decision.Domain == failureDomainCredential",
		"decision.OutlierScope",
	} {
		if strings.Contains(source, retired) {
			t.Fatalf("Compact still consumes direct/compat effect via %q", retired)
		}
	}

	for _, invariant := range []string{
		"maxSameChannelRetries = group.MaxRetries",
		"resolveSidepathDirective(coordination.Disposition)",
	} {
		if !strings.Contains(source, invariant) {
			t.Fatalf("P5C changed Compact retry invariant %q", invariant)
		}
	}
	for _, forbidden := range []string{
		"balancer.TryAcquireChannel(",
		"balancer.TryConsumeChannelRPM(",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("P5C must not add Compact policy %q", forbidden)
		}
	}
}

func TestP5CImagesPersistsRoutingTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"upstream service temporarily unavailable"}}`)
	}))
	defer server.Close()

	channel := newImagesTestChannel("p5c-image-trace", server.URL)
	group := &model.Group{Name: "p5c-image-trace-group", Mode: model.GroupModeFailover}
	persistImagesRoute(t, ctx, group, channel)

	logs := op.RelayLogSubscribe()
	t.Cleanup(func() { op.RelayLogUnsubscribe(logs) })

	recorder, c := newImagesTestContext(
		"/v1/images/generations",
		[]byte(`{"model":"p5c-image-trace-group","prompt":"draw"}`),
		"application/json",
	)
	c.Set("api_key_id", 5501)
	ImagesHandler("/images/generations", c)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}

	attempt := p5cFirstRealAttempt(t, p5cWaitRelayLog(t, logs, group.Name))
	if attempt.FailureDomain != "provider_transient" {
		t.Fatalf("failure domain=%q, want provider_transient", attempt.FailureDomain)
	}
	if attempt.RuleID == "" || attempt.RetryDirective == "" {
		t.Fatalf("routing trace incomplete: %+v", attempt.AttemptRoutingTrace)
	}
	if attempt.RuntimeEffect != "provider_cooldown" {
		t.Fatalf("runtime effect=%q, want provider_cooldown", attempt.RuntimeEffect)
	}
	if attempt.OutlierEffect != "provider_failure" {
		t.Fatalf("outlier effect=%q, want provider_failure", attempt.OutlierEffect)
	}
	if attempt.CredentialRevision <= 0 {
		t.Fatalf("credential revision=%d, want persisted revision", attempt.CredentialRevision)
	}
}

func TestP5CCompactPersistsRoutingTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	availability.Reset()
	ctx := setupRelayTestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeP4C1BInvalidCredential(w)
	}))
	defer server.Close()

	route := newP4C1BCompactRoute(t, ctx, server.URL, "p5c-trace", []model.ChannelKey{{
		Enabled: true, ChannelKey: "p5c-compact-bad-key",
	}})

	logs := op.RelayLogSubscribe()
	t.Cleanup(func() { op.RelayLogUnsubscribe(logs) })

	recorder := runP4C1BCompact(t, route.group.Name, 5502)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401; body=%s", recorder.Code, recorder.Body.String())
	}

	attempt := p5cFirstRealAttempt(t, p5cWaitRelayLog(t, logs, route.group.Name))
	if attempt.FailureDomain != "credential" {
		t.Fatalf("failure domain=%q, want credential", attempt.FailureDomain)
	}
	if attempt.FailureScope != "credential" {
		t.Fatalf("failure scope=%q, want credential", attempt.FailureScope)
	}
	if attempt.RetryDirective != "rotate_credential" {
		t.Fatalf("retry directive=%q, want rotate_credential", attempt.RetryDirective)
	}
	if attempt.RuntimeEffect != "credential_cooldown" {
		t.Fatalf("runtime effect=%q, want credential_cooldown", attempt.RuntimeEffect)
	}
	if attempt.OutlierEffect != "none" {
		t.Fatalf("outlier effect=%q, want none", attempt.OutlierEffect)
	}
	if attempt.CredentialRevision <= 0 {
		t.Fatalf("credential revision=%d, want persisted revision", attempt.CredentialRevision)
	}
}
