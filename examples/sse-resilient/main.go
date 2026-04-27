// Command sse-resilient demonstrates sugaar's resilient SSE handler:
// heartbeats, client-retry hint, and Last-Event-ID resume backed by the
// Hub replay buffer.
//
//	go run ./examples/sse-resilient
//
// Then in another shell, watch the stream:
//
//	curl -N http://localhost:8080/sse/agents/42
//
// Reconnect from a known event ID:
//
//	curl -N -H 'Last-Event-ID: 5' http://localhost:8080/sse/agents/42
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/eneko/sugaar"
)

func main() {
	app := sugaar.New(sugaar.Options{
		Addr:   ":8080",
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})

	// Keep the last 100 events per topic for resume.
	app.Hub.EnableReplay(100)

	app.GET("/", func(c *sugaar.Context) error {
		return c.String(200, "open /sse/agents/42 with curl -N or an EventSource client\n")
	})

	app.GET("/sse/agents/{id}", app.SSETopicWith(
		func(c *sugaar.Context) string { return "agents." + c.Param("id") },
		sugaar.SSEOptions{
			Heartbeat:    5 * time.Second,
			ClientRetry:  2 * time.Second,
			Buffer:       128,
			WriteTimeout: 10 * time.Second,
			OnDrop: func(c *sugaar.Context, ev sugaar.Event) {
				slog.Warn("dropped event for slow client",
					"path", c.R().URL.Path, "type", ev.Type)
			},
		},
	))

	// Synthetic agent: emits a "thought" once per second on agents.42.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		i := 0
		for range t.C {
			i++
			app.Hub.Publish(sugaar.Event{
				Topic: "agents.42",
				Type:  "thought",
				Data:  fmt.Sprintf("step %d", i),
			})
		}
	}()

	if err := app.Run(context.Background()); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
