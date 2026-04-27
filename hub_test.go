package sugaar

import (
	"sync"
	"testing"
	"time"
)

func TestHubFanOut(t *testing.T) {
	h := NewHub(nil)
	defer h.Close()

	a, cancelA := h.Subscribe("agents", 4)
	b, cancelB := h.Subscribe("agents", 4)
	defer cancelA()
	defer cancelB()

	if got := h.SubscriberCount("agents"); got != 2 {
		t.Fatalf("subscriber count = %d, want 2", got)
	}

	delivered := h.Publish(Event{Topic: "agents", Type: "thought", Data: "hi"})
	if delivered != 2 {
		t.Fatalf("delivered = %d, want 2", delivered)
	}

	for _, sub := range []*Subscription{a, b} {
		select {
		case ev := <-sub.Events():
			if ev.Type != "thought" {
				t.Fatalf("type = %q", ev.Type)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}
	}
}

func TestHubDropsSlowSubscribers(t *testing.T) {
	h := NewHub(nil)
	defer h.Close()

	_, cancel := h.Subscribe("t", 1) // tiny buffer, never drained
	defer cancel()

	for i := 0; i < 10; i++ {
		h.Publish(Event{Topic: "t", Type: "x"})
	}
	if h.DropCount() == 0 {
		t.Fatal("expected drops on slow subscriber")
	}
}

func TestHubUnsubscribeClosesChannel(t *testing.T) {
	h := NewHub(nil)
	sub, cancel := h.Subscribe("t", 1)
	cancel()
	if _, ok := <-sub.Events(); ok {
		t.Fatal("channel should be closed after cancel")
	}
}

func TestHubConcurrent(t *testing.T) {
	h := NewHub(nil)
	defer h.Close()

	const subs = 20
	const events = 100
	var wg sync.WaitGroup
	for i := 0; i < subs; i++ {
		s, cancel := h.Subscribe("x", events)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			received := 0
			for range s.Events() {
				received++
				if received == events {
					return
				}
			}
		}()
	}
	// give subscribers a moment to register
	for h.SubscriberCount("x") < subs {
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < events; i++ {
		h.Publish(Event{Topic: "x", Type: "tick"})
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscribers did not all receive events")
	}
}
