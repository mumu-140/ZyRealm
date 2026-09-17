package grouphealth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/outlierwindow"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestProbeOutcomeHTTPClassification(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   ProbeOutcome
	}{
		{name: "ok", status: http.StatusOK, want: ProbeOutcomeSuccess},
		{name: "no content", status: http.StatusNoContent, want: ProbeOutcomeSuccess},
		{name: "credential rejected", status: http.StatusUnauthorized, want: ProbeOutcomeCredentialRejected},
		{name: "provider rejected", status: http.StatusForbidden, want: ProbeOutcomeRejected},
		{name: "not found", status: http.StatusNotFound, want: ProbeOutcomeRejected},
		{name: "bad request", status: http.StatusBadRequest, want: ProbeOutcomeRejected},
		{name: "rate limited", status: http.StatusTooManyRequests, want: ProbeOutcomeRateLimited},
		{name: "request timeout", status: http.StatusRequestTimeout, want: ProbeOutcomeUnavailable},
		{name: "internal error", status: http.StatusInternalServerError, want: ProbeOutcomeUnavailable},
		{name: "bad gateway", status: http.StatusBadGateway, want: ProbeOutcomeUnavailable},
		{name: "service unavailable", status: http.StatusServiceUnavailable, want: ProbeOutcomeUnavailable},
		{name: "gateway timeout", status: http.StatusGatewayTimeout, want: ProbeOutcomeUnavailable},
		{name: "no response", status: 0, want: ProbeOutcomeInconclusive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyProbeHTTP(tt.status); got != tt.want {
				t.Fatalf("classifyProbeHTTP(%d)=%q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestProbeOutcomeTransportClassification(t *testing.T) {
	if got := classifyProbeTransport(context.Canceled, errors.New("transport stopped")); got != ProbeOutcomeInconclusive {
		t.Fatalf("parent cancellation=%q, want %q", got, ProbeOutcomeInconclusive)
	}
	if got := classifyProbeTransport(nil, context.DeadlineExceeded); got != ProbeOutcomeUnavailable {
		t.Fatalf("probe deadline=%q, want %q", got, ProbeOutcomeUnavailable)
	}
}

func TestProberDoesNotReportPassiveHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	channel := model.Channel{
		ID:       987654,
		Type:     outbound.OutboundTypeOpenAIChat,
		BaseUrls: []model.BaseUrl{{URL: server.URL}},
	}
	usedKey := model.ChannelKey{ID: 1, ChannelKey: "sk-diagnostic-test"}
	modelName := "diagnostic-neutrality-model"
	outlierwindow.ClearChannel(channel.ID)
	t.Cleanup(func() { outlierwindow.ClearChannel(channel.ID) })

	result := (&Prober{CandidateTimeout: time.Second}).RunCandidate(context.Background(), channel, usedKey, modelName)
	if !result.Success {
		t.Fatalf("diagnostic probe failed unexpectedly: status=%d err=%s", result.HTTPStatus, result.ErrorMessage)
	}
	if stats := outlierwindow.Evaluate(channel.ID, modelName, time.Now()); stats.Samples != 0 {
		t.Fatalf("active diagnostic must not enter passive health window: samples=%d", stats.Samples)
	}
}
