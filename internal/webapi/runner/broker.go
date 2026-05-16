// Package runner provides in-process pub/sub for run events. Used by the
// HTTP Phase 2 mutating endpoints to fan out the EventSink from one engine
// goroutine to N SSE subscribers (browser tabs watching the same run).
//
// Events are live-only — no persistence, no replay. RunLogs-based replay
// is a future phase once the inventory.RunLogs repo is implemented.
package runner

import (
	"sync"

	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

// RunBroker is in-process pub/sub keyed by deployment ID. One broker
// instance per nimbusfab-server process; subscribers come and go as SSE
// clients connect/disconnect.
type RunBroker struct {
	mu      sync.Mutex
	subs    map[string][]chan provisioner.RunEvent // deploymentID → list of subscriber chans
	bufSize int
}

// NewRunBroker returns a broker; bufSize is the per-subscriber buffer used
// when dispatching events. Small buffer = backpressure on slow clients
// (currently events are dropped silently for slow subs; could detach the
// sub in a richer impl).
func NewRunBroker(bufSize int) *RunBroker {
	if bufSize <= 0 {
		bufSize = 64
	}
	return &RunBroker{subs: map[string][]chan provisioner.RunEvent{}, bufSize: bufSize}
}

// Subscribe returns a channel that receives events for deploymentID until
// the returned unsubscribe func is called. Subscribers see events posted
// AFTER Subscribe returns (no replay).
func (b *RunBroker) Subscribe(deploymentID string) (<-chan provisioner.RunEvent, func()) {
	ch := make(chan provisioner.RunEvent, b.bufSize)
	b.mu.Lock()
	b.subs[deploymentID] = append(b.subs[deploymentID], ch)
	b.mu.Unlock()
	return ch, func() { b.unsubscribe(deploymentID, ch) }
}

// Publisher returns a channel the engine writes RunEvents into for
// deploymentID. When the caller closes the channel, all current
// subscribers are closed (signals "operation done"; SSE handlers emit a
// "complete" event and end the connection).
func (b *RunBroker) Publisher(deploymentID string) chan<- provisioner.RunEvent {
	pubCh := make(chan provisioner.RunEvent, b.bufSize)
	go func() {
		for evt := range pubCh {
			b.dispatch(deploymentID, evt)
		}
		b.closeSubs(deploymentID)
	}()
	return pubCh
}

func (b *RunBroker) dispatch(deploymentID string, evt provisioner.RunEvent) {
	b.mu.Lock()
	targets := append([]chan provisioner.RunEvent(nil), b.subs[deploymentID]...)
	b.mu.Unlock()
	for _, ch := range targets {
		select {
		case ch <- evt:
		default:
			// Slow subscriber: drop. A richer impl could detach the sub
			// after N drops; Phase 2 keeps the contract simple.
		}
	}
}

func (b *RunBroker) closeSubs(deploymentID string) {
	b.mu.Lock()
	targets := b.subs[deploymentID]
	delete(b.subs, deploymentID)
	b.mu.Unlock()
	for _, ch := range targets {
		close(ch)
	}
}

// unsubscribe removes ch from the deployment's sub list. Does NOT close ch
// — the publisher's closeSubs will close it when the operation ends; a
// double-close would panic.
func (b *RunBroker) unsubscribe(deploymentID string, ch chan provisioner.RunEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	list := b.subs[deploymentID]
	for i, c := range list {
		if c == ch {
			b.subs[deploymentID] = append(list[:i], list[i+1:]...)
			return
		}
	}
}
