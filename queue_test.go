package sugaar

import (
	"testing"
	"time"
)

func TestQueueEnqueueDequeue(t *testing.T) {
	q := NewQueue()

	q.Enqueue(Event{Topic: "a", Type: "x"})
	q.Enqueue(Event{Topic: "a", Type: "y"})
	q.Enqueue(Event{Topic: "b", Type: "z"})

	if got := q.Len("a"); got != 2 {
		t.Fatalf("len(a) = %d, want 2", got)
	}
	if got := q.Len("b"); got != 1 {
		t.Fatalf("len(b) = %d, want 1", got)
	}

	ev, ok := q.Dequeue("a")
	if !ok || ev.Type != "x" {
		t.Fatalf("first dequeue = %v, %v", ev, ok)
	}
	ev, ok = q.Dequeue("a")
	if !ok || ev.Type != "y" {
		t.Fatalf("second dequeue = %v, %v", ev, ok)
	}
	_, ok = q.Dequeue("a")
	if ok {
		t.Fatal("expected empty dequeue")
	}
	if got := q.Len("a"); got != 0 {
		t.Fatalf("len(a) after drain = %d, want 0", got)
	}
}

func TestQueuePeek(t *testing.T) {
	q := NewQueue()
	q.Enqueue(Event{Topic: "t", Type: "first"})

	ev, ok := q.Peek("t")
	if !ok || ev.Type != "first" {
		t.Fatalf("peek = %v, %v", ev, ok)
	}
	if q.Len("t") != 1 {
		t.Fatal("peek should not remove event")
	}

	_, ok = q.Peek("missing")
	if ok {
		t.Fatal("expected peek on empty topic to fail")
	}
}

func TestQueueTopics(t *testing.T) {
	q := NewQueue()
	q.Enqueue(Event{Topic: "foo", Type: "a"})
	q.Enqueue(Event{Topic: "bar", Type: "b"})

	topics := q.Topics()
	if len(topics) != 2 {
		t.Fatalf("topics = %v, want 2", topics)
	}
}

func TestQueueDrain(t *testing.T) {
	q := NewQueue()
	q.Enqueue(Event{Topic: "t", Type: "1"})
	q.Enqueue(Event{Topic: "t", Type: "2"})

	out := q.Drain("t")
	if len(out) != 2 {
		t.Fatalf("drain len = %d, want 2", len(out))
	}
	if out[0].Type != "1" || out[1].Type != "2" {
		t.Fatalf("drain order wrong: %v", out)
	}
	if q.Len("t") != 0 {
		t.Fatal("expected empty after drain")
	}
}

func TestQueueBroadcast(t *testing.T) {
	h := NewHub(nil)
	defer h.Close()

	sub, cancel := h.Subscribe("agents", 4)
	defer cancel()

	q := NewQueue()
	q.Enqueue(Event{Topic: "agents", Type: "a"})
	q.Enqueue(Event{Topic: "agents", Type: "b"})

	delivered := q.Broadcast("agents", h)
	if delivered != 2 {
		t.Fatalf("delivered = %d, want 2", delivered)
	}

	for _, want := range []string{"a", "b"} {
		select {
		case ev := <-sub.Events():
			if ev.Type != want {
				t.Fatalf("type = %q, want %q", ev.Type, want)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for broadcast event")
		}
	}

	if q.Len("agents") != 0 {
		t.Fatal("queue should be empty after broadcast")
	}
}

func TestQueueBroadcastAll(t *testing.T) {
	h := NewHub(nil)
	defer h.Close()

	subA, cancelA := h.Subscribe("a", 4)
	defer cancelA()
	subB, cancelB := h.Subscribe("b", 4)
	defer cancelB()

	q := NewQueue()
	q.Enqueue(Event{Topic: "a", Type: "1"})
	q.Enqueue(Event{Topic: "b", Type: "2"})

	delivered := q.BroadcastAll(h)
	if delivered != 2 {
		t.Fatalf("delivered = %d, want 2", delivered)
	}

	select {
	case ev := <-subA.Events():
		if ev.Type != "1" {
			t.Fatalf("a type = %q", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event on a")
	}

	select {
	case ev := <-subB.Events():
		if ev.Type != "2" {
			t.Fatalf("b type = %q", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event on b")
	}

	if len(q.Topics()) != 0 {
		t.Fatal("queue should be empty after broadcast all")
	}
}

func TestQueueConcurrent(t *testing.T) {
	q := NewQueue()
	const n = 100

	// concurrent enqueues
	for i := 0; i < n; i++ {
		go func(i int) {
			q.Enqueue(Event{Topic: "x", Type: "tick"})
		}(i)
	}

	// wait a bit then drain
	time.Sleep(50 * time.Millisecond)
	out := q.Drain("x")
	if len(out) != n {
		t.Fatalf("drained %d, want %d", len(out), n)
	}
}
