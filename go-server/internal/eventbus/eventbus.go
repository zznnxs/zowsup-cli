// Package eventbus is a tiny in-process pub/sub used to fan out account
// events (connection state, incoming messages, presence updates, ...) to
// HTTP/WebSocket clients without coupling the account manager directly to
// the API layer.
package eventbus

import (
	"sync"
	"sync/atomic"
	"time"
)

// Event is a typed payload pushed onto the bus.
//
// Topic is a free-form string; the API layer translates it to a websocket
// event name. AccountID identifies which account the event pertains to
// (zero for server-wide events).
type Event struct {
	Topic     string
	AccountID int64
	Payload   any
	At        time.Time
}

// Subscriber is a buffered receiver for events. Cancel returns a func that
// detaches the subscription and drains C.
type Subscriber struct {
	C      <-chan Event
	cancel func()
}

// Cancel unsubscribes and closes C.
func (s *Subscriber) Cancel() { s.cancel() }

// Bus is the central pub/sub. Safe for concurrent use.
type Bus struct {
	mu      sync.RWMutex
	subs    map[int64]chan Event
	nextID  atomic.Int64
	bufSize int
}

// New returns a fresh bus. Each subscriber receives through a buffered
// channel of bufSize events; if a slow subscriber stalls past that buffer
// the publisher drops the event for that subscriber (logged elsewhere).
func New(bufSize int) *Bus {
	if bufSize <= 0 {
		bufSize = 64
	}
	return &Bus{subs: make(map[int64]chan Event), bufSize: bufSize}
}

// Subscribe registers a new subscriber and returns its channel.
func (b *Bus) Subscribe() *Subscriber {
	id := b.nextID.Add(1)
	ch := make(chan Event, b.bufSize)
	b.mu.Lock()
	b.subs[id] = ch
	b.mu.Unlock()
	cancelled := atomic.Bool{}
	cancel := func() {
		if cancelled.Swap(true) {
			return
		}
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
		close(ch)
	}
	return &Subscriber{C: ch, cancel: cancel}
}

// Publish sends ev to every current subscriber. Slow subscribers that are
// full at the moment of publish get the event silently dropped — they will
// see subsequent events normally once they catch up.
func (b *Bus) Publish(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Close cancels every active subscriber. Subsequent Publish calls are
// no-ops.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, ch := range b.subs {
		delete(b.subs, id)
		close(ch)
	}
}
