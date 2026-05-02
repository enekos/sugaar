package sugaar

import "sync"

// Queue is a minimal in-memory FIFO queue for Events, keyed by topic.
// It buffers events when subscribers are not yet ready and can later
// broadcast them through a Hub or be consumed directly.
type Queue struct {
	mu     sync.Mutex
	queues map[string][]Event
}

// NewQueue creates an empty Queue.
func NewQueue() *Queue {
	return &Queue{queues: make(map[string][]Event)}
}

// Enqueue adds ev to the tail of its topic queue.
func (q *Queue) Enqueue(ev Event) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.queues[ev.Topic] = append(q.queues[ev.Topic], ev)
}

// Dequeue removes and returns the oldest event for topic.
// ok is false when the topic queue is empty.
func (q *Queue) Dequeue(topic string) (Event, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	vals := q.queues[topic]
	if len(vals) == 0 {
		return Event{}, false
	}
	ev := vals[0]
	if len(vals) == 1 {
		delete(q.queues, topic)
	} else {
		q.queues[topic] = vals[1:]
	}
	return ev, true
}

// Peek returns the oldest event for topic without removing it.
// ok is false when the topic queue is empty.
func (q *Queue) Peek(topic string) (Event, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	vals := q.queues[topic]
	if len(vals) == 0 {
		return Event{}, false
	}
	return vals[0], true
}

// Len returns the number of queued events for topic.
func (q *Queue) Len(topic string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queues[topic])
}

// Topics returns all topics that currently have queued events.
// Order is undefined.
func (q *Queue) Topics() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.queues))
	for t := range q.queues {
		out = append(out, t)
	}
	return out
}

// Drain removes and returns all queued events for topic in FIFO order.
func (q *Queue) Drain(topic string) []Event {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.queues[topic]
	delete(q.queues, topic)
	return out
}

// Broadcast drains all queued events for topic into hub.
// It returns the total number of subscribers that received the events
// (as reported by Hub.Publish).
func (q *Queue) Broadcast(topic string, hub *Hub) int {
	events := q.Drain(topic)
	delivered := 0
	for _, ev := range events {
		delivered += hub.Publish(ev)
	}
	return delivered
}

// BroadcastAll drains all queued events for all topics into hub.
// It returns the total number of subscriber deliveries across all events.
func (q *Queue) BroadcastAll(hub *Hub) int {
	q.mu.Lock()
	all := q.queues
	q.queues = make(map[string][]Event)
	q.mu.Unlock()

	delivered := 0
	for _, events := range all {
		for _, ev := range events {
			delivered += hub.Publish(ev)
		}
	}
	return delivered
}
