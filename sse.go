package sugaar

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SSEOptions tunes the resilience knobs for [App.SSETopic].
//
// All fields are optional; the zero value yields sensible defaults suited
// for streaming agentic events behind a typical reverse proxy.
type SSEOptions struct {
	// Buffer is the per-subscription channel buffer. Larger buffers absorb
	// bursts at the cost of memory; smaller buffers cause faster drop
	// detection. Defaults to 256.
	Buffer int

	// Heartbeat is the interval between SSE comment frames sent to keep the
	// connection alive through idle proxies and detect half-open peers.
	// Set to a negative value to disable. Defaults to 15s.
	Heartbeat time.Duration

	// ClientRetry, when non-zero, is sent as the SSE "retry:" directive on
	// connect. The browser uses this as the reconnect backoff. Defaults to
	// 3s.
	ClientRetry time.Duration

	// WriteTimeout bounds each write. A stuck/slow client will not block the
	// publishing goroutine forever. Requires the underlying connection to
	// support deadlines (it does for net/http). Defaults to 10s.
	WriteTimeout time.Duration

	// OnDrop, if set, is invoked once per dropped event for this connection
	// so handlers can take action (log, mark client as out-of-sync, force
	// disconnect). The drop counter on the Subscription is always updated
	// regardless.
	OnDrop func(c *Context, ev Event)
}

func (o SSEOptions) withDefaults() SSEOptions {
	if o.Buffer <= 0 {
		o.Buffer = 256
	}
	if o.Heartbeat == 0 {
		o.Heartbeat = 15 * time.Second
	}
	if o.ClientRetry == 0 {
		o.ClientRetry = 3 * time.Second
	}
	if o.WriteTimeout == 0 {
		o.WriteTimeout = 10 * time.Second
	}
	return o
}

// SSETopic streams Hub events for the resolved topic as Server-Sent Events
// with default resilience options. See [App.SSETopicWith] for tuning.
//
//	app.GET("/sse/agent/{id}", app.SSETopic(func(c *sugaar.Context) string {
//	    return "agent." + c.Param("id")
//	}))
func (a *App) SSETopic(topicFn func(*Context) string) HandlerFunc {
	return a.SSETopicWith(topicFn, SSEOptions{})
}

// SSETopicWith returns an SSE handler with the given options. It honours the
// Last-Event-ID header (and the equivalent ?lastEventId= query parameter)
// to resume from the Hub replay buffer when one is enabled.
func (a *App) SSETopicWith(topicFn func(*Context) string, opts SSEOptions) HandlerFunc {
	opts = opts.withDefaults()
	return func(c *Context) error {
		flusher, ok := c.W().(http.Flusher)
		if !ok {
			return fmt.Errorf("streaming unsupported")
		}

		h := c.W().Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache, no-transform")
		h.Set("Connection", "keep-alive")
		h.Set("X-Accel-Buffering", "no")
		c.W().WriteHeader(http.StatusOK)

		rc := http.NewResponseController(c.W())
		writeDeadline := func() {
			if opts.WriteTimeout > 0 {
				_ = rc.SetWriteDeadline(time.Now().Add(opts.WriteTimeout))
			}
		}

		// Initial frame: retry hint + open comment to flush headers.
		writeDeadline()
		if opts.ClientRetry > 0 {
			fmt.Fprintf(c.W(), "retry: %d\n", opts.ClientRetry.Milliseconds())
		}
		fmt.Fprint(c.W(), ": open\n\n")
		flusher.Flush()

		topic := topicFn(c)
		lastID := lastEventID(c)
		sub, cancel := a.Hub.SubscribeSince(topic, opts.Buffer, lastID)
		defer cancel()

		var ticker *time.Ticker
		var heartbeat <-chan time.Time
		if opts.Heartbeat > 0 {
			ticker = time.NewTicker(opts.Heartbeat)
			defer ticker.Stop()
			heartbeat = ticker.C
		}

		ctx := c.Ctx()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-heartbeat:
				writeDeadline()
				if _, err := fmt.Fprint(c.W(), ": ping\n\n"); err != nil {
					return nil
				}
				flusher.Flush()
			case ev, ok := <-sub.Events():
				if !ok {
					return nil
				}
				writeDeadline()
				if err := writeSSE(c.W(), ev); err != nil {
					return nil
				}
				flusher.Flush()
				if d := sub.Drops.Load(); d > 0 && opts.OnDrop != nil {
					opts.OnDrop(c, ev)
				}
			}
		}
	}
}

// lastEventID resolves the Last-Event-ID per the SSE spec, with a
// query-parameter fallback for clients (curl, EventSource polyfills) that
// can't easily set headers on reconnect.
func lastEventID(c *Context) string {
	if v := strings.TrimSpace(c.Header("Last-Event-ID")); v != "" {
		return v
	}
	return strings.TrimSpace(c.Query("lastEventId"))
}

func writeSSE(w http.ResponseWriter, ev Event) error {
	if ev.ID != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", ev.ID); err != nil {
			return err
		}
	}
	if ev.Type != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", ev.Type); err != nil {
			return err
		}
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}
