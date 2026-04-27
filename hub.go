package sugaar

import (
	"log/slog"
	"sync"
)

// Hub is a topic-based fan-out for Events. It is the core primitive for
// streaming agentic events to many connected clients.
//
// Subscribers receive events on a buffered channel. If a subscriber is too
// slow and its buffer fills, the Hub drops the event for that subscriber and
// increments DropCount; this protects fast subscribers from head-of-line
// blocking by slow ones.
type Hub struct {
	log *slog.Logger

	mu     sync.RWMutex
	subs   map[string]map[*Subscription]struct{} // topic -> set
	closed bool

	replaySize int
	replays    map[string]*replayBuffer

	DropCount uint64 // atomic-ish: read under mu
}

// Subscription is a single subscriber. Use Events to receive, and call Close
// (or use the cancel returned by Subscribe) to detach.
type Subscription struct {
	topic  string
	ch     chan Event
	hub    *Hub
	closed bool
	mu     sync.Mutex
}

// Events returns the receive channel. Closed when the subscription ends.
func (s *Subscription) Events() <-chan Event { return s.ch }

// NewHub creates an empty Hub.
func NewHub(log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{log: log, subs: make(map[string]map[*Subscription]struct{})}
}

// Subscribe registers a subscriber for the given topic. The buffer controls
// how many events may queue before drops begin; pick based on expected burst
// size. cancel detaches the subscription and closes the events channel.
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
		default: // buffer too small for replay; skip rather than block
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

// Publish delivers ev to every subscriber of ev.Topic. Returns the number of
// subscribers reached (events successfully enqueued).
func (h *Hub) Publish(ev Event) int {
	h.mu.RLock()
	set := h.subs[ev.Topic]
	subs := make([]*Subscription, 0, len(set))
	for s := range set {
		subs = append(subs, s)
	}
	h.mu.RUnlock()

	if h.replaySize > 0 {
		h.mu.Lock()
		rb := h.replays[ev.Topic]
		if rb == nil {
			rb = newReplayBuffer(h.replaySize)
			h.replays[ev.Topic] = rb
		}
		h.mu.Unlock()
		rb.push(ev)
	}

	delivered := 0
	for _, s := range subs {
		select {
		case s.ch <- ev:
			delivered++
		default:
			h.mu.Lock()
			h.DropCount++
			h.mu.Unlock()
			h.log.Warn("sugaar: event dropped (slow subscriber)", "topic", ev.Topic)
		}
	}
	return delivered
}

// SubscriberCount returns the number of subscribers for topic.
func (h *Hub) SubscriberCount(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[topic])
}

// Close terminates all subscriptions. Subsequent Subscribe returns a closed sub.
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	all := h.subs
	h.subs = make(map[string]map[*Subscription]struct{})
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
