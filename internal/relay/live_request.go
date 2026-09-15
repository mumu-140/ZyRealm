package relay

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var errManualInterrupt = errors.New("manual relay interrupt")

type liveRequestPhase string

const (
	livePhaseRouting      liveRequestPhase = "routing"
	livePhaseAttempting   liveRequestPhase = "attempting"
	livePhaseStreaming    liveRequestPhase = "streaming"
	livePhaseInterrupting liveRequestPhase = "interrupting"
)

type LiveRequestSnapshot struct {
	RequestID           string `json:"request_id"`
	StartedAt           int64  `json:"started_at"`
	Transport           string `json:"transport"`
	APIKeyID            int    `json:"api_key_id"`
	RequestedModel      string `json:"requested_model"`
	ChannelID           int    `json:"channel_id"`
	ChannelKeyID        int    `json:"channel_key_id"`
	IngressProtocol     string `json:"ingress_protocol"`
	UpstreamProtocol    string `json:"upstream_protocol"`
	ProviderAttempt     int    `json:"provider_attempt"`
	WireAttempt         int    `json:"wire_attempt"`
	DispatchState       string `json:"dispatch_state"`
	DownstreamCommitted bool   `json:"downstream_committed"`
	Phase               string `json:"phase"`
	InterruptRequested  bool   `json:"interrupt_requested"`
}

type relayControl struct {
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelCauseFunc
	snapshot LiveRequestSnapshot
}

type liveRequestRegistry struct {
	mu     sync.RWMutex
	active map[string]*relayControl
}

var (
	liveRequestSequence atomic.Uint64
	liveRequests        = liveRequestRegistry{active: make(map[string]*relayControl)}
)

func newRelayControl(parent context.Context, initial LiveRequestSnapshot) *relayControl {
	if parent == nil {
		parent = context.Background()
	}
	if initial.RequestID == "" {
		initial.RequestID = nextLiveRequestID()
	}
	if initial.StartedAt == 0 {
		initial.StartedAt = time.Now().UnixMilli()
	}
	if initial.Phase == "" {
		initial.Phase = string(livePhaseRouting)
	}
	if initial.DispatchState == "" {
		initial.DispatchState = dispatchStateString(dispatchNotSent)
	}
	ctx, cancel := context.WithCancelCause(parent)
	return &relayControl{ctx: ctx, cancel: cancel, snapshot: initial}
}

func nextLiveRequestID() string {
	return fmt.Sprintf("lr_%d_%d", time.Now().UnixMilli(), liveRequestSequence.Add(1))
}

func (c *relayControl) Context() context.Context {
	if c == nil || c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

func (c *relayControl) Snapshot() LiveRequestSnapshot {
	if c == nil {
		return LiveRequestSnapshot{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

func (c *relayControl) Update(fn func(*LiveRequestSnapshot)) {
	if c == nil || fn == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(&c.snapshot)
}

func (c *relayControl) Interrupt() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	if c.snapshot.InterruptRequested {
		c.mu.Unlock()
		return false
	}
	c.snapshot.InterruptRequested = true
	c.snapshot.Phase = string(livePhaseInterrupting)
	cancel := c.cancel
	c.mu.Unlock()

	if cancel != nil {
		cancel(errManualInterrupt)
	}
	return true
}

func registerLiveRequest(control *relayControl) {
	if control == nil {
		return
	}
	id := control.Snapshot().RequestID
	if id == "" {
		return
	}
	liveRequests.mu.Lock()
	liveRequests.active[id] = control
	liveRequests.mu.Unlock()
}

func unregisterLiveRequest(id string) {
	if id == "" {
		return
	}
	liveRequests.mu.Lock()
	delete(liveRequests.active, id)
	liveRequests.mu.Unlock()
}

func ListLiveRequests() []LiveRequestSnapshot {
	liveRequests.mu.RLock()
	controls := make([]*relayControl, 0, len(liveRequests.active))
	for _, control := range liveRequests.active {
		controls = append(controls, control)
	}
	liveRequests.mu.RUnlock()

	snapshots := make([]LiveRequestSnapshot, 0, len(controls))
	for _, control := range controls {
		snapshots = append(snapshots, control.Snapshot())
	}
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].StartedAt == snapshots[j].StartedAt {
			return snapshots[i].RequestID > snapshots[j].RequestID
		}
		return snapshots[i].StartedAt > snapshots[j].StartedAt
	})
	return snapshots
}

func InterruptLiveRequest(id string) bool {
	if id == "" {
		return false
	}
	liveRequests.mu.RLock()
	control, ok := liveRequests.active[id]
	liveRequests.mu.RUnlock()
	if !ok {
		return false
	}
	control.Interrupt()
	return true
}
