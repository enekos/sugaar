package sugaar

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// DefaultUpgrader applies a same-origin CheckOrigin: requests without an
// Origin header (curl, native clients) are accepted, but cross-origin browser
// upgrades are rejected. Use AllowOrigins to opt into specific cross-origin
// callers, or supply a custom upgrader via UpgradeWSWith.
var DefaultUpgrader = websocket.Upgrader{
	ReadBufferSize:  4 << 10,
	WriteBufferSize: 4 << 10,
	CheckOrigin:     SameOriginCheck,
}

// SameOriginCheck accepts WebSocket upgrades where the Origin host matches
// Host, or where Origin is absent (non-browser clients). It is the default
// for [DefaultUpgrader] to mitigate cross-site WebSocket hijacking (CSWSH).
func SameOriginCheck(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// AllowOrigins returns a CheckOrigin that accepts requests with an Origin
// matching one of the given values exactly (scheme + host). The empty list
// behaves like [SameOriginCheck]. "*" disables the check (development only).
func AllowOrigins(origins ...string) func(*http.Request) bool {
	if len(origins) == 0 {
		return SameOriginCheck
	}
	allow := make(map[string]struct{}, len(origins))
	wildcard := false
	for _, o := range origins {
		if o == "*" {
			wildcard = true
		}
		allow[o] = struct{}{}
	}
	return func(r *http.Request) bool {
		if wildcard {
			return true
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		_, ok := allow[origin]
		return ok
	}
}

// WSConn is a thin wrapper around *websocket.Conn applying a per-write
// deadline. Returned to handlers that opt in via App.UpgradeWS.
type WSConn struct {
	*websocket.Conn
	WriteTimeout time.Duration
}

// WriteJSON encodes v as JSON under the configured deadline.
func (c *WSConn) WriteJSON(v any) error {
	if c.WriteTimeout > 0 {
		_ = c.SetWriteDeadline(time.Now().Add(c.WriteTimeout))
	}
	return c.Conn.WriteJSON(v)
}

// UpgradeWS upgrades the request to a WebSocket using DefaultUpgrader.
// The returned WSConn must be closed by the caller.
func (a *App) UpgradeWS(c *Context) (*WSConn, error) {
	return a.UpgradeWSWith(c, DefaultUpgrader)
}

// UpgradeWSWith is UpgradeWS with a custom upgrader.
func (a *App) UpgradeWSWith(c *Context, up websocket.Upgrader) (*WSConn, error) {
	conn, err := up.Upgrade(c.W(), c.R(), nil)
	if err != nil {
		return nil, err
	}
	return &WSConn{Conn: conn, WriteTimeout: 10 * time.Second}, nil
}

// StreamTopic upgrades to a WebSocket and forwards every Hub event matching
// the topic returned by topicFn to the client. Returns when the client
// disconnects, the request context is cancelled, or the Hub closes.
//
//	app.GET("/ws/agent/{id}", app.StreamTopic(func(c *sugaar.Context) string {
//	    return "agent." + c.Param("id")
//	}))
func (a *App) StreamTopic(topicFn func(*Context) string) HandlerFunc {
	return a.StreamTopicWith(topicFn, DefaultUpgrader)
}

// StreamTopicWith is StreamTopic with a custom upgrader.
func (a *App) StreamTopicWith(topicFn func(*Context) string, up websocket.Upgrader) HandlerFunc {
	return func(c *Context) error {
		ws, err := a.UpgradeWSWith(c, up)
		if err != nil {
			// Upgrader already wrote a status (e.g. 403 on origin reject);
			// don't return an error or the framework will overwrite it.
			return nil
		}
		// Closing the conn unblocks the reader goroutine via NextReader,
		// guaranteeing it exits before the handler returns.
		defer ws.Close()

		topic := topicFn(c)
		sub, cancel := a.Hub.Subscribe(topic, 256)
		defer cancel()

		clientGone := make(chan struct{})
		go func() {
			defer close(clientGone)
			ws.SetReadLimit(1 << 20)
			_ = ws.SetReadDeadline(time.Now().Add(60 * time.Second))
			ws.SetPongHandler(func(string) error {
				return ws.SetReadDeadline(time.Now().Add(60 * time.Second))
			})
			for {
				if _, _, err := ws.NextReader(); err != nil {
					return
				}
			}
		}()

		ping := time.NewTicker(30 * time.Second)
		defer ping.Stop()

		for {
			select {
			case <-clientGone:
				return nil
			case <-c.Ctx().Done():
				// Closing the conn aborts the in-flight NextReader so the
				// reader goroutine exits; we still wait for it to avoid a
				// brief leak of the goroutine past the handler.
				_ = ws.Close()
				<-clientGone
				return nil
			case ev, ok := <-sub.Events():
				if !ok {
					return nil
				}
				if err := ws.WriteJSON(ev); err != nil {
					return nil // client gone — defer closes conn, reader unblocks
				}
			case <-ping.C:
				_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
					return nil
				}
			}
		}
	}
}
