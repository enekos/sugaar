package sugaar

import "sync"

// replayBuffer is a per-topic ring buffer of recent events. When a new
// subscriber joins, the buffered events are delivered before any new ones,
// so a client that briefly disconnects can resume without gaps.
type replayBuffer struct {
	mu   sync.Mutex
	size int
	ring []Event
	head int // next write index
	full bool
}

func newReplayBuffer(size int) *replayBuffer {
	return &replayBuffer{size: size, ring: make([]Event, size)}
}

func (b *replayBuffer) push(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ring[b.head] = ev
	b.head = (b.head + 1) % b.size
	if b.head == 0 {
		b.full = true
	}
}

// snapshot returns the buffer contents in chronological order.
func (b *replayBuffer) snapshot() []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.full {
		out := make([]Event, b.head)
		copy(out, b.ring[:b.head])
		return out
	}
	out := make([]Event, b.size)
	copy(out, b.ring[b.head:])
	copy(out[b.size-b.head:], b.ring[:b.head])
	return out
}

// EnableReplay turns on a per-topic ring buffer of the most recent size
// events. New subscribers receive the buffered events first. Call once,
// before Subscribe; subsequent calls resize the buffer for new topics only.
func (h *Hub) EnableReplay(size int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.replaySize = size
	if h.replays == nil {
		h.replays = make(map[string]*replayBuffer)
	}
}

// Replay returns a snapshot of the buffered events for topic. Returns nil
// when replay is disabled or the topic has no buffer yet.
func (h *Hub) Replay(topic string) []Event {
	h.mu.RLock()
	rb := h.replays[topic]
	h.mu.RUnlock()
	if rb == nil {
		return nil
	}
	return rb.snapshot()
}
