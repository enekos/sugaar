package sugaar

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// readSSEFrame reads up to a blank-line separator from r and returns the
// frame text. Returns ("", false) on read error.
func readSSEFrame(t *testing.T, br *bufio.Reader) (string, bool) {
	t.Helper()
	var sb strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return sb.String(), false
		}
		sb.WriteString(line)
		if line == "\n" || line == "\r\n" {
			return sb.String(), true
		}
	}
}

func newSSEServer(t *testing.T, opts SSEOptions, replay int) (*App, *httptest.Server) {
	t.Helper()
	app := New(Options{DisablePprof: true})
	if replay > 0 {
		app.Hub.EnableReplay(replay)
	}
	app.GET("/sse", app.SSETopicWith(func(c *Context) string { return "t" }, opts))
	srv := httptest.NewServer(app)
	t.Cleanup(srv.Close)
	t.Cleanup(app.Hub.Close)
	return app, srv
}

func TestSSEDeliversEvent(t *testing.T) {
	app, srv := newSSEServer(t, SSEOptions{Heartbeat: -1, ClientRetry: -1}, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/sse", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}

	br := bufio.NewReader(resp.Body)
	// Read open comment.
	if frame, ok := readSSEFrame(t, br); !ok || !strings.Contains(frame, ": open") {
		t.Fatalf("expected open frame, got %q", frame)
	}

	// Wait for the subscription to register before publishing.
	deadline := time.Now().Add(time.Second)
	for app.Hub.SubscriberCount("t") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	app.Hub.Publish(Event{Topic: "t", Type: "thought", Data: "hi"})

	frame, ok := readSSEFrame(t, br)
	if !ok {
		t.Fatalf("expected event frame, got %q", frame)
	}
	if !strings.Contains(frame, "event: thought") {
		t.Fatalf("missing event line: %q", frame)
	}
	if !strings.Contains(frame, `"data":"hi"`) {
		t.Fatalf("missing data payload: %q", frame)
	}
}

func TestSSESendsRetryAndHeartbeat(t *testing.T) {
	_, srv := newSSEServer(t, SSEOptions{Heartbeat: 30 * time.Millisecond, ClientRetry: 1500 * time.Millisecond}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/sse", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)

	frame, ok := readSSEFrame(t, br)
	if !ok || !strings.Contains(frame, "retry: 1500") || !strings.Contains(frame, ": open") {
		t.Fatalf("expected retry+open, got %q", frame)
	}
	frame, ok = readSSEFrame(t, br)
	if !ok || !strings.Contains(frame, ": ping") {
		t.Fatalf("expected heartbeat ping, got %q", frame)
	}
}

func TestSSEResumesFromLastEventID(t *testing.T) {
	app, srv := newSSEServer(t, SSEOptions{Heartbeat: -1, ClientRetry: -1}, 8)

	// Pre-publish three events. IDs auto-assigned: "1","2","3".
	app.Hub.Publish(Event{Topic: "t", Type: "a", Data: 1})
	app.Hub.Publish(Event{Topic: "t", Type: "b", Data: 2})
	app.Hub.Publish(Event{Topic: "t", Type: "c", Data: 3})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/sse", nil)
	req.Header.Set("Last-Event-ID", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)

	// Open frame.
	if _, ok := readSSEFrame(t, br); !ok {
		t.Fatal("missing open frame")
	}
	// Then events 2, 3 — not 1.
	for _, want := range []string{"event: b", "event: c"} {
		frame, ok := readSSEFrame(t, br)
		if !ok || !strings.Contains(frame, want) {
			t.Fatalf("want %q, got %q", want, frame)
		}
	}
}

func TestSSEStopsOnClientDisconnect(t *testing.T) {
	app, srv := newSSEServer(t, SSEOptions{Heartbeat: -1, ClientRetry: -1}, 0)

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/sse", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(resp.Body)
	if _, ok := readSSEFrame(t, br); !ok {
		t.Fatal("missing open frame")
	}

	deadline := time.Now().Add(time.Second)
	for app.Hub.SubscriberCount("t") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	resp.Body.Close()

	deadline = time.Now().Add(2 * time.Second)
	for app.Hub.SubscriberCount("t") > 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber not cleaned up after disconnect")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSSEOnDropFires(t *testing.T) {
	var dropped atomic.Uint64
	opts := SSEOptions{
		Heartbeat:   -1,
		ClientRetry: -1,
		Buffer:      1,
		OnDrop: func(_ *Context, _ Event) {
			dropped.Add(1)
		},
	}
	app, srv := newSSEServer(t, opts, 0)

	// Connect but don't read aggressively — pause the reader to fill buffer.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/sse", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	// Drain open frame so handler can proceed.
	if _, ok := readSSEFrame(t, br); !ok {
		t.Fatal("missing open frame")
	}

	deadline := time.Now().Add(time.Second)
	for app.Hub.SubscriberCount("t") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no subscriber")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Burst — the handler reads one at a time but with tiny buffer, drops
	// will accumulate at hub level once buffer is full.
	for i := 0; i < 200; i++ {
		app.Hub.Publish(Event{Topic: "t", Type: "x"})
	}

	deadline = time.Now().Add(2 * time.Second)
	for app.Hub.DropCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("expected drops, got none")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Force the handler to deliver at least one event so OnDrop fires.
	for i := 0; i < 5; i++ {
		if _, ok := readSSEFrame(t, br); !ok {
			break
		}
		if dropped.Load() > 0 {
			break
		}
	}
	if dropped.Load() == 0 {
		t.Fatal("OnDrop callback never fired")
	}
}
