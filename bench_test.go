package sugaar_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eneko/sugaar"
)

func BenchmarkRouteHelloJSON(b *testing.B) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.GET("/u/{id}", func(c *sugaar.Context) error {
		return c.JSON(200, map[string]string{"id": c.Param("id")})
	})
	req := httptest.NewRequest("GET", "/u/42", nil)
	rec := &nullWriter{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkHubFanout(b *testing.B) {
	h := sugaar.NewHub(nil)
	defer h.Close()
	const subs = 64
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

// nullWriter discards everything for benchmarks.
type nullWriter struct {
	h      http.Header
	status int
}

func (n *nullWriter) Header() http.Header {
	if n.h == nil {
		n.h = http.Header{}
	}
	return n.h
}
func (n *nullWriter) WriteHeader(s int)           { n.status = s }
func (n *nullWriter) Write(b []byte) (int, error) { return len(b), nil }
