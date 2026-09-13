package relay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
)

func TestSendRequestCanceledBeforeTransportRemainsNotSent(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	ra := &relayAttempt{channel: &dbmodel.Channel{}}

	_, err = ra.sendRequest(req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sendRequest error = %v, want context.Canceled", err)
	}
	if ra.dispatchState != dispatchNotSent {
		t.Fatalf("dispatch state = %v, want NOT_SENT", ra.dispatchState)
	}
	if hits.Load() != 0 {
		t.Fatalf("pre-canceled request reached upstream %d times, want 0", hits.Load())
	}
}

func TestSendRequestCanceledAfterUpstreamReceivesRequestIsMaybeSent(t *testing.T) {
	received := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(received)
		<-r.Context().Done()
	}))
	defer upstream.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	ra := &relayAttempt{channel: &dbmodel.Channel{}}

	errCh := make(chan error, 1)
	go func() {
		_, sendErr := ra.sendRequest(req)
		errCh <- sendErr
	}()

	<-received
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("sendRequest error = %v, want context.Canceled", err)
	}
	if ra.dispatchState != dispatchMaybeSent {
		t.Fatalf("dispatch state = %v, want MAYBE_SENT after upstream received request", ra.dispatchState)
	}
}
