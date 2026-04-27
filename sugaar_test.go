package sugaar_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eneko/sugaar"
	"github.com/eneko/sugaar/golden"
	"github.com/gorilla/websocket"
)

func newTestApp() *sugaar.App {
	return sugaar.New(sugaar.Options{DisablePprof: true})
}

func TestRouteAndJSON(t *testing.T) {
	app := newTestApp()
	app.GET("/hello/{name}", func(c *sugaar.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"hello": c.Param("name")})
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/hello/eneko", nil))
	golden.Assert(t, "hello", golden.Response(rec.Result()))
}

func TestErrorHandlerReturns500(t *testing.T) {
	app := newTestApp()
	app.GET("/boom", func(c *sugaar.Context) error {
		return io.ErrUnexpectedEOF
	})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/boom", nil))
	if rec.Code != 500 {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unexpected EOF") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestRecoverPanic(t *testing.T) {
	app := newTestApp()
	app.GET("/panic", func(c *sugaar.Context) error {
		panic("nope")
	})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/panic", nil))
	if rec.Code != 500 {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSSEStreamsHubEvents(t *testing.T) {
	app := newTestApp()
	app.GET("/sse/{topic}", app.SSETopic(func(c *sugaar.Context) string {
		return c.Param("topic")
	}))

	srv := httptest.NewServer(app)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/sse/agents", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// give the server a moment to register the subscriber
	for i := 0; i < 50 && app.Hub.SubscriberCount("agents") == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	app.Hub.Publish(sugaar.Event{Topic: "agents", Type: "thought", Data: "ping"})

	buf := make([]byte, 256)
	deadline := time.Now().Add(2 * time.Second)
	var seen string
	for time.Now().Before(deadline) {
		n, _ := resp.Body.Read(buf)
		seen += string(buf[:n])
		if strings.Contains(seen, "event: thought") {
			break
		}
	}
	if !strings.Contains(seen, "event: thought") || !strings.Contains(seen, `"data":"ping"`) {
		t.Fatalf("did not see expected SSE frame, got: %q", seen)
	}
}

func TestWebSocketStreamsHubEvents(t *testing.T) {
	app := newTestApp()
	app.GET("/ws/{topic}", app.StreamTopic(func(c *sugaar.Context) string {
		return c.Param("topic")
	}))

	srv := httptest.NewServer(app)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/agents"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	for i := 0; i < 50 && app.Hub.SubscriberCount("agents") == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	app.Hub.Publish(sugaar.Event{Topic: "agents", Type: "thought", Data: "hi"})

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(msg), `"type":"thought"`) {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestMiddlewareRunsInOrder(t *testing.T) {
	app := newTestApp()
	var mu sync.Mutex
	var order []string
	mark := func(name string) sugaar.Middleware {
		return func(next sugaar.HandlerFunc) sugaar.HandlerFunc {
			return func(c *sugaar.Context) error {
				mu.Lock()
				order = append(order, "in:"+name)
				mu.Unlock()
				err := next(c)
				mu.Lock()
				order = append(order, "out:"+name)
				mu.Unlock()
				return err
			}
		}
	}
	app.Use(mark("a"), mark("b"))
	app.GET("/x", func(c *sugaar.Context) error { return c.String(200, "ok") })

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))

	got := strings.Join(order, ",")
	// recover and request log are registered first, so a and b are inside them.
	if !strings.Contains(got, "in:a,in:b,out:b,out:a") {
		t.Fatalf("order = %v", order)
	}
}
