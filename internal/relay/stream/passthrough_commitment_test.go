package stream

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type chunksThenDelayedEOFSource struct {
	chunks [][]byte
	index  int
	delay  time.Duration
}

func (s *chunksThenDelayedEOFSource) ReadEvent(ctx context.Context) ([]byte, error) {
	if s.index < len(s.chunks) {
		chunk := s.chunks[s.index]
		s.index++
		return chunk, nil
	}

	timer := time.NewTimer(s.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *chunksThenDelayedEOFSource) Close() error { return nil }

func assertPassthroughCommentsDoNotSatisfyFirstToken(t *testing.T, chunks ...[]byte) {
	t.Helper()

	source := &chunksThenDelayedEOFSource{chunks: chunks, delay: 250 * time.Millisecond}
	writer := newMockStreamWriter()
	var firstTokenCalled atomic.Bool
	payloadObserver := NewSSEPayloadObserver()

	processor := NewStreamProcessor(StreamConfig{
		Source:            source,
		Writer:            writer,
		Context:           context.Background(),
		FirstTokenTimeout: 25 * time.Millisecond,
		PayloadObserver:   payloadObserver.Observe,
		BufferRawStream:   true,
		OnFirstToken: func() {
			firstTokenCalled.Store(true)
		},
	})

	err := processor.Run()
	if err == nil || !strings.Contains(err.Error(), "first token timeout") {
		t.Fatalf("expected comment-only passthrough to remain under first-token timeout, got %v", err)
	}
	if firstTokenCalled.Load() {
		t.Fatal("SSE comment must not trigger OnFirstToken")
	}
	if processor.PayloadWritten() {
		t.Fatal("SSE comment must not establish semantic payload commitment")
	}

	var want strings.Builder
	for _, chunk := range chunks {
		want.Write(chunk)
	}
	if got := writer.buffer.String(); got != want.String() {
		t.Fatalf("passthrough bytes changed while classifying commitment: got %q want %q", got, want.String())
	}
}

func TestStreamProcessor_PassthroughSSECommentDoesNotSatisfyFirstToken(t *testing.T) {
	assertPassthroughCommentsDoNotSatisfyFirstToken(t, []byte(": keepalive\n\n"))
}

func TestStreamProcessor_PassthroughSSECommentSplitAcrossRawChunksDoesNotSatisfyFirstToken(t *testing.T) {
	assertPassthroughCommentsDoNotSatisfyFirstToken(t,
		[]byte(": keep"),
		[]byte("alive\r"),
		[]byte("\n\r"),
		[]byte("\n"),
	)
}

func TestStreamProcessor_PassthroughSSEDataStillSatisfiesFirstToken(t *testing.T) {
	chunks := [][]byte{
		[]byte(": keepalive\n\n"),
		[]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\r"),
		[]byte("\n\r\n"),
	}
	source := newMockStreamSource(chunks)
	writer := newMockStreamWriter()
	var firstTokenCalls atomic.Int32
	payloadObserver := NewSSEPayloadObserver()

	processor := NewStreamProcessor(StreamConfig{
		Source:          source,
		Writer:          writer,
		Context:         context.Background(),
		PayloadObserver: payloadObserver.Observe,
		BufferRawStream: true,
		OnFirstToken: func() {
			firstTokenCalls.Add(1)
		},
	})

	if err := processor.Run(); err != nil {
		t.Fatalf("expected data-bearing passthrough stream to succeed, got %v", err)
	}
	if !processor.PayloadWritten() {
		t.Fatal("data-bearing SSE event must establish semantic payload commitment")
	}
	if got := firstTokenCalls.Load(); got != 1 {
		t.Fatalf("expected OnFirstToken exactly once, got %d", got)
	}

	var want strings.Builder
	for _, chunk := range chunks {
		want.Write(chunk)
	}
	if got := writer.buffer.String(); got != want.String() {
		t.Fatalf("passthrough bytes changed while classifying payload: got %q want %q", got, want.String())
	}
}
