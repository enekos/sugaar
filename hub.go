package sugaar

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
)

// Hub is a topic-based fan-out for Events. It is the core primitive for
// streaming agentic events to many connected clients.
//
// Subscribers receive events on a buffered channel. If a subscriber is too
// slow and its buffer fills, the Hub drops the event for that subscriber and
// increments DropCount; this protects fast subscribers from head-of-line
// blocking by slow ones.
//
// When [Hub.EnableReplay] has been called, every published Event without an
// ID is assigned a monotonic per-hub identifier so clients can resume from a
// known point via Last-Event-ID.
type Hub struct {
	log *slog.Logger

	mu     sync.RWMutex
	subs   map[string]map[*Subscription]struct{} // topic -> set
	closed bool

	replaySize int
	replays    map[string]*replayBuffer

	dropCount atomic.Uint64
	idSeq     atomic.Uint64
}

// Subscription is a single subscriber. Use Events to receive, and call Close
// (or use the cancel returned by Subscribe) to detach.
//
// Slow subscribers do not block the hub: when the channel buffer fills, the
// hub drops events and bumps the subscription's Drops counter (also reflected
// on Hub.DropCount). Drops are observable so handlers can decide whether to
// reset state or notify the client.
type Subscription struct {
	topic  string
	ch     chan Event
	hub    *Hub
	closed bool
	mu     sync.Mutex

	Drops atomic.Uint64
}

// Events returns the receive channel. Closed when the subscription ends.
func (s *Subscription) Events() <-chan Event { return s.ch }

// Topic returns the topic the subscription is bound to.
func (s *Subscription) Topic() string { return s.topic }

// NewHub creates an empty Hub.
func NewHub(log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{log: log, subs: make(map[string]map[*Subscription]struct{})}
}

// DropCount returns the total number of events dropped across all
// subscribers. Safe to call concurrently.
func (h *Hub) DropCount() uint64 { return h.dropCount.Load() }

// Subscribe registers a subscriber for the given topic. The buffer controls
// how many events may queue before drops begin; pick based on expected burst
// size. cancel detaches the subscription and closes the events channel.
//
// If replay is enabled the subscriber receives buffered events first, in
// chronological order. Replay events that don't fit in the buffer are
// dropped (and counted) to preserve the no-block guarantee.
func (h *Hub) Subscribe(topic string, buffer int) (*Subscription, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	sub := &Subscription{topic: topic, ch: make(chan Event, buffer), hub: h}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		close(sub.ch)
		sub.closed = true
		return sub, func() {}
	}
	if h.subs[topic] == nil {
		h.subs[topic] = make(map[*Subscription]struct{})
	}
	h.subs[topic][sub] = struct{}{}
	var replay []Event
	if rb := h.replays[topic]; rb != nil {
		replay = rb.snapshot()
	}
	h.mu.Unlock()

	for _, ev := range replay {
		select {
		case sub.ch <- ev:
		default:
			sub.Drops.Add(1)
			h.dropCount.Add(1)
		}
	}

	return sub, func() { h.unsubscribe(sub) }
}

// SubscribeSince is like Subscribe but only delivers replayed events whose
// IDs are strictly greater than lastID. Useful for SSE resume via the
// Last-Event-ID header. lastID may be empty, in which case it behaves like
// Subscribe.
func (h *Hub) SubscribeSince(topic string, buffer int, lastID string) (*Subscription, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	sub := &Subscription{topic: topic, ch: make(chan Event, buffer), hub: h}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		close(sub.ch)
		sub.closed = true
		return sub, func() {}
	}
	if h.subs[topic] == nil {
		h.subs[topic] = make(map[*Subscription]struct{})
	}
	h.subs[topic][sub] = struct{}{}
	var replay []Event
	if rb := h.replays[topic]; rb != nil {
		replay = rb.since(lastID)
	}
	h.mu.Unlock()

	for _, ev := range replay {
		select {
		case sub.ch <- ev:
		default:
			sub.Drops.Add(1)
			h.dropCount.Add(1)
		}
	}

	return sub, func() { h.unsubscribe(sub) }
}

func (h *Hub) unsubscribe(sub *Subscription) {
	h.mu.Lock()
	if set, ok := h.subs[sub.topic]; ok {
		delete(set, sub)
		if len(set) == 0 {
			delete(h.subs, sub.topic)
		}
	}
	h.mu.Unlock()

	sub.mu.Lock()
	if !sub.closed {
		close(sub.ch)
		sub.closed = true
	}
	sub.mu.Unlock()
}

// Publish delivers ev to every subscriber of ev.Topic. If ev.ID is empty and
// replay is enabled, an ID is auto-assigned so resumption works without
// caller bookkeeping. Returns the number of subscribers reached (events
// successfully enqueued).
func (h *Hub) Publish(ev Event) int {
	if ev.ID == "" && h.replaySize > 0 {
		ev.ID = strconv.FormatUint(h.idSeq.Add(1), 10)
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return 0
	}
	set := h.subs[ev.Topic]
	subs := make([]*Subscription, 0, len(set))
	for s := range set {
		subs = append(subs, s)
	}
	var rb *replayBuffer
	if h.replaySize > 0 {
		rb = h.replays[ev.Topic]
		if rb == nil {
			rb = newReplayBuffer(h.replaySize)
			if h.replays == nil {
				h.replays = make(map[string]*replayBuffer)
			}
			h.replays[ev.Topic] = rb
		}
	}
	h.mu.Unlock()

	if rb != nil {
		rb.push(ev)
	}

	delivered := 0
	for _, s := range subs {
		if s.send(ev) {
			delivered++
		} else {
			h.dropCount.Add(1)
			h.log.LogAttrs(context.Background(), slog.LevelWarn, "sugaar: event dropped (slow subscriber)", slog.String("topic", ev.Topic))
		}
	}
	return delivered
}

// send delivers ev to the subscriber without blocking, returning true on
// success. Holding sub.mu serialises against unsubscribe/close so Hub.Close
// cannot panic Publish with a send-on-closed-channel.
func (s *Subscription) send(ev Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.ch <- ev:
		return true
	default:
		s.Drops.Add(1)
		return false
	}
}

// SubscriberCount returns the number of subscribers for topic.
func (h *Hub) SubscriberCount(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[topic])
}

// Topics returns the topics that currently have at least one subscriber.
// Order is undefined.
func (h *Hub) Topics() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.subs))
	for t := range h.subs {
		out = append(out, t)
	}
	return out
}

// Close terminates all subscriptions. Subsequent Subscribe returns a closed
// sub, and concurrent Publish drops events instead of panicking.
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	all := h.subs
	h.subs = make(map[string]map[*Subscription]struct{})
	h.replays = nil
	h.mu.Unlock()

	for _, set := range all {
		for s := range set {
			s.mu.Lock()
			if !s.closed {
				close(s.ch)
				s.closed = true
			}
			s.mu.Unlock()
		}
	}
}
