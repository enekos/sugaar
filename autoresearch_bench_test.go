package sugaar_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eneko/sugaar"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func BenchmarkRouteHelloJSONQuiet(b *testing.B) {
	app := sugaar.New(sugaar.Options{DisablePprof: true, Logger: discardLogger()})
	app.GET("/u/{id}", func(c *sugaar.Context) error {
		return c.JSON(200, map[string]string{"id": c.Param("id")})
	})
	req := httptest.NewRequest("GET", "/u/42", nil)
	rec := &benchNullWriter{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkRouteStringQuiet(b *testing.B) {
	app := sugaar.New(sugaar.Options{DisablePprof: true, Logger: discardLogger()})
	app.GET("/hello", func(c *sugaar.Context) error {
		return c.String(200, "hello world")
	})
	req := httptest.NewRequest("GET", "/hello", nil)
	rec := &benchNullWriter{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkRouteWithMiddlewareQuiet(b *testing.B) {
	app := sugaar.New(sugaar.Options{DisablePprof: true, Logger: discardLogger()})
	app.Use(sugaar.RequestID())
	app.GET("/api/data", func(c *sugaar.Context) error {
		return c.JSON(200, map[string]any{"ok": true})
	})
	req := httptest.NewRequest("GET", "/api/data", nil)
	rec := &benchNullWriter{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkHubFanoutSmallQuiet(b *testing.B) {
	h := sugaar.NewHub(discardLogger())
	defer h.Close()
	const subs = 16
	for i := 0; i < subs; i++ {
		_, cancel := h.Subscribe("t", 1024)
		defer cancel()
	}
	ev := sugaar.Event{Topic: "t", Type: "tick"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Publish(ev)
	}
}

func BenchmarkHubFanoutLargeQuiet(b *testing.B) {
	h := sugaar.NewHub(discardLogger())
	defer h.Close()
	const subs = 256
	for i := 0; i < subs; i++ {
		_, cancel := h.Subscribe("t", 1024)
		defer cancel()
	}
	ev := sugaar.Event{Topic: "t", Type: "tick"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Publish(ev)
	}
}

func BenchmarkHubSubscribeUnsubscribeQuiet(b *testing.B) {
	h := sugaar.NewHub(discardLogger())
	defer h.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, cancel := h.Subscribe("t", 64)
		cancel()
	}
}

func BenchmarkSSEWriteEventQuiet(b *testing.B) {
	ev := sugaar.Event{Topic: "t", Type: "tick", Data: map[string]string{"msg": "hello"}}
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// writeSSE is unexported; we benchmark via the public handler path
		// For now, test the JSON marshal + fmt path indirectly
		rec.Body.Reset()
		_ = writeSSEWrapper(rec, ev)
	}
}

// benchNullWriter discards everything for benchmarks.
type benchNullWriter struct {
	h      http.Header
	status int
}

func (n *benchNullWriter) Header() http.Header {
	if n.h == nil {
		n.h = http.Header{}
	}
	return n.h
}
func (n *benchNullWriter) WriteHeader(s int)           { n.status = s }
func (n *benchNullWriter) Write(b []byte) (int, error) { return len(b), nil }

// writeSSEWrapper mirrors the formatting logic for benchmarking.
func writeSSEWrapper(w http.ResponseWriter, ev sugaar.Event) error {
	// We can't call the unexported writeSSE, so we approximate the work
	data, _ := json.Marshal(ev)
	_, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, data)
	return err
}
