// Command agent-stream is a runnable example of a sugaar server that
// publishes synthetic agentic events to a Hub topic and exposes them over
// HTTP, SSE, and WebSocket.
//
//	go run ./examples/agent-stream
//	curl -N localhost:8080/sse/agents/42
//	wscat -c ws://localhost:8080/ws/agents/42
package main

import (
	"context"
	"log"
	"time"

	"github.com/eneko/sugaar"
)

func main() {
	app := sugaar.New(sugaar.Options{Addr: ":8080"})

	app.GET("/healthz", func(c *sugaar.Context) error {
		return c.String(200, "ok")
	})

	topicFor := func(c *sugaar.Context) string {
		return "agents." + c.Param("id")
	}
	app.GET("/sse/agents/{id}", app.SSETopic(topicFor))
	app.GET("/ws/agents/{id}", app.StreamTopic(topicFor))

	app.POST("/agents/{id}/emit", func(c *sugaar.Context) error {
		var ev sugaar.Event
		if err := c.BindJSON(&ev); err != nil {
			return err
		}
		ev.Topic = "agents." + c.Param("id")
		app.Hub.Publish(ev)
		return c.JSON(200, map[string]int{"subscribers": app.Hub.SubscriberCount(ev.Topic)})
	})

	// Demo emitter: every 2s, publish a tick to agents.demo.
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		i := 0
		for range t.C {
			i++
			app.Hub.Publish(sugaar.Event{
				Topic: "agents.demo",
				Type:  "tick",
				Data:  map[string]int{"n": i},
			})
		}
	}()

	if err := app.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
