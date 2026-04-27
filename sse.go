package sugaar

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSETopic streams Hub events for the resolved topic as Server-Sent Events.
// HTTP-friendly counterpart to StreamTopic for EventSource / curl clients.
//
//	app.GET("/sse/agent/{id}", app.SSETopic(func(c *sugaar.Context) string {
//	    return "agent." + c.Param("id")
//	}))
func (a *App) SSETopic(topicFn func(*Context) string) HandlerFunc {
	return func(c *Context) error {
		flusher, ok := c.W().(http.Flusher)
		if !ok {
			return fmt.Errorf("streaming unsupported")
		}
		h := c.W().Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("Connection", "keep-alive")
		h.Set("X-Accel-Buffering", "no")
		c.W().WriteHeader(http.StatusOK)
		flusher.Flush()

		topic := topicFn(c)
		sub, cancel := a.Hub.Subscribe(topic, 256)
		defer cancel()

		ctx := c.Ctx()
		for {
			select {
			case <-ctx.Done():
				return nil
			case ev, ok := <-sub.Events():
				if !ok {
					return nil
				}
				if err := writeSSE(c.W(), ev); err != nil {
					return nil
				}
				flusher.Flush()
			}
		}
	}
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
