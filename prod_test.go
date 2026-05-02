package sugaar_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/eneko/sugaar"
	"github.com/gorilla/websocket"
)

func TestHubCloseRaceWithPublish(t *testing.T) {
	// Regression for the send-on-closed-channel panic that used to fire when
	// Close ran concurrently with in-flight Publish.
	hub := sugaar.NewHub(nil)
	const subs = 16
	for i := 0; i < subs; i++ {
		_, _ = hub.Subscribe("t", 1) // intentionally never drained
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			hub.Publish(sugaar.Event{Topic: "t", Type: "x"})
		}
	}()
	go func() {
		defer wg.Done()
		hub.Close()
	}()
	wg.Wait()
}

func TestBindJSONRejectsOversizedBody(t *testing.T) {
	app := sugaar.New(sugaar.Options{
		DisablePprof:  true,
		DisableHealth: true,
		MaxBodyBytes:  64,
	})
	app.POST("/echo", func(c *sugaar.Context) error {
		var v map[string]string
		if err := c.BindJSON(&v); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, v)
	})

	// Valid JSON, just very long, so the decoder reads past the cap before
	// rejecting on parse — exercising MaxBytesReader instead of a parse error.
	payload := append([]byte(`{"k":"`), bytes.Repeat([]byte("a"), 256)...)
	payload = append(payload, []byte(`"}`)...)
	req := httptest.NewRequest("POST", "/echo", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHealthzMountedByDefault(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestHealthzCanBeDisabled(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true, DisableHealth: true})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPprofRequiresLoopbackByDefault(t *testing.T) {
	app := sugaar.New(sugaar.Options{}) // pprof on, default loopback gate

	// Non-loopback caller is forbidden.
	req := httptest.NewRequest("GET", "/debug/pprof/heap", nil)
	req.RemoteAddr = "10.0.0.5:54321"
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-loopback pprof status = %d, want 403", rec.Code)
	}

	// Loopback caller is allowed through to pprof.
	req2 := httptest.NewRequest("GET", "/debug/pprof/cmdline", nil)
	req2.RemoteAddr = "127.0.0.1:54321"
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("loopback pprof status = %d, want 200", rec2.Code)
	}
}

func TestSameOriginRejectsCrossOrigin(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.GET("/ws", app.StreamTopic(func(c *sugaar.Context) string { return "t" }))

	srv := httptest.NewServer(app)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	headers := http.Header{}
	headers.Set("Origin", "http://attacker.example")
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err == nil {
		t.Fatal("expected upgrade rejection from cross-origin caller")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("response = %v", resp)
	}
}

func TestSameOriginAcceptsMatchingHost(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.GET("/ws", app.StreamTopic(func(c *sugaar.Context) string { return "t" }))

	srv := httptest.NewServer(app)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	headers := http.Header{}
	headers.Set("Origin", srv.URL) // same host as the test server
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("same-origin upgrade failed: %v", err)
	}
	_ = conn.Close()
}
