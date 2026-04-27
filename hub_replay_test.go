package sugaar

import (
	"strconv"
	"testing"
	"time"
)

func TestReplayWrapsAround(t *testing.T) {
	h := NewHub(nil)
	defer h.Close()
	h.EnableReplay(3)

	for i := 1; i <= 5; i++ {
		h.Publish(Event{Topic: "t", Type: strconv.Itoa(i)})
	}

	got := h.Replay("t")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	wantTypes := []string{"3", "4", "5"}
	for i, ev := range got {
		if ev.Type != wantTypes[i] {
			t.Fatalf("[%d] type = %q, want %q", i, ev.Type, wantTypes[i])
		}
	}
}

func TestReplaySinceFiltersByID(t *testing.T) {
	h := NewHub(nil)
	defer h.Close()
	h.EnableReplay(8)

	for i := 1; i <= 4; i++ {
		h.Publish(Event{Topic: "t", Type: strconv.Itoa(i)})
	}

	got := h.ReplaySince("t", "2")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "3" || got[1].ID != "4" {
		t.Fatalf("ids = %q,%q want 3,4", got[0].ID, got[1].ID)
	}
}

func TestReplaySinceUnknownIDReturnsAll(t *testing.T) {
	h := NewHub(nil)
	defer h.Close()
	h.EnableReplay(4)
	h.Publish(Event{Topic: "t", Type: "a"})
	h.Publish(Event{Topic: "t", Type: "b"})

	got := h.ReplaySince("t", "does-not-exist")
	if len(got) != 2 {
		t.Fatalf("expected full replay when ID unknown, got %d", len(got))
	}
}

func TestSubscribeSinceDeliversOnlyNewer(t *testing.T) {
	h := NewHub(nil)
	defer h.Close()
	h.EnableReplay(4)
	h.Publish(Event{Topic: "t", Type: "a"})
	h.Publish(Event{Topic: "t", Type: "b"})
	h.Publish(Event{Topic: "t", Type: "c"})

	sub, cancel := h.SubscribeSince("t", 8, "1")
	defer cancel()

	want := []string{"b", "c"}
	for _, w := range want {
		select {
		case ev := <-sub.Events():
			if ev.Type != w {
				t.Fatalf("got %q want %q", ev.Type, w)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for %q", w)
		}
	}
}

func TestPublishAssignsMonotonicIDs(t *testing.T) {
	h := NewHub(nil)
	defer h.Close()
	h.EnableReplay(4)
	h.Publish(Event{Topic: "t"})
	h.Publish(Event{Topic: "t"})

	got := h.Replay("t")
	if got[0].ID != "1" || got[1].ID != "2" {
		t.Fatalf("ids = %q,%q want 1,2", got[0].ID, got[1].ID)
	}
}

func TestPublishKeepsExistingID(t *testing.T) {
	h := NewHub(nil)
	defer h.Close()
	h.EnableReplay(4)
	h.Publish(Event{Topic: "t", ID: "custom"})

	got := h.Replay("t")
	if got[0].ID != "custom" {
		t.Fatalf("id = %q want custom", got[0].ID)
	}
}
