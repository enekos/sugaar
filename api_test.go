package sugaar_test

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eneko/sugaar"
	"github.com/eneko/sugaar/sugaartest"
)

func TestHTTPErrorMapsStatus(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.GET("/missing", func(c *sugaar.Context) error {
		return sugaar.NotFound("user").WithCode("user_not_found")
	})
	c := sugaartest.New(app)
	resp := c.GET("/missing")
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := sugaartest.Body(resp)
	if !strings.Contains(body, `"code":"user_not_found"`) || !strings.Contains(body, `"error":"user"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestGroupAndMiddlewareScope(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	hits := 0
	tag := func(name string) sugaar.Middleware {
		return func(next sugaar.HandlerFunc) sugaar.HandlerFunc {
			return func(c *sugaar.Context) error {
				hits++
				c.W().Header().Add("X-Tag", name)
				return next(c)
			}
		}
	}
	api := app.Group("/api", tag("api"))
	v1 := api.Group("/v1", tag("v1"))
	v1.GET("/ping", func(c *sugaar.Context) error { return c.String(200, "pong") })

	c := sugaartest.New(app)
	resp := c.GET("/api/v1/ping")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	tags := resp.Header.Values("X-Tag")
	if len(tags) != 2 || tags[0] != "api" || tags[1] != "v1" {
		t.Fatalf("X-Tag = %v", tags)
	}
}

func TestRequestIDPropagates(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.RequestID())
	app.GET("/who", func(c *sugaar.Context) error {
		return c.String(200, c.RequestID())
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/who", nil)
	req.Header.Set("X-Request-Id", "abc123")
	app.ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-Id") != "abc123" {
		t.Fatalf("missing request id header")
	}
	if rec.Body.String() != "abc123" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestCORSPreflight(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.CORS(sugaar.CORSOptions{AllowOrigins: []string{"https://x.test"}}))
	app.GET("/x", func(c *sugaar.Context) error { return c.String(200, "ok") })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/x", nil)
	req.Header.Set("Origin", "https://x.test")
	app.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("preflight code = %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://x.test" {
		t.Fatalf("allow-origin missing: %v", rec.Header())
	}
}

func TestBearerAuth(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	g := app.Group("/private", sugaar.BearerAuth("s3cret"))
	g.GET("/me", func(c *sugaar.Context) error { return c.String(200, "ok") })

	c := sugaartest.New(app)
	if r := c.GET("/private/me"); r.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r.StatusCode)
	}
	authed := c.With("Authorization", "Bearer s3cret")
	if r := authed.GET("/private/me"); r.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", r.StatusCode)
	}
}

func TestTimeoutMiddleware(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.GET("/slow", sugaar.Timeout(20*time.Millisecond)(func(c *sugaar.Context) error {
		select {
		case <-c.Ctx().Done():
			return c.Ctx().Err()
		case <-time.After(200 * time.Millisecond):
			return c.String(200, "late")
		}
	}))
	c := sugaartest.New(app)
	resp := c.GET("/slow")
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestGZipCompresses(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.GZip())
	app.GET("/big", func(c *sugaar.Context) error {
		return c.String(200, strings.Repeat("hello ", 200))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/big", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	app.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(zr)
	if !strings.HasPrefix(string(body), "hello ") {
		t.Fatalf("decoded body = %q", string(body)[:30])
	}
}

func TestBindQueryAndForm(t *testing.T) {
	type Q struct {
		Name string `query:"name"`
		Age  int    `query:"age"`
		On   bool   `query:"on"`
	}
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.GET("/search", func(c *sugaar.Context) error {
		var q Q
		if err := c.BindQuery(&q); err != nil {
			return sugaar.BadRequest(err.Error())
		}
		return c.JSON(200, q)
	})
	c := sugaartest.New(app)
	resp := c.GET("/search?name=ada&age=37&on=true")
	body := sugaartest.Body(resp)
	if !strings.Contains(body, `"Name":"ada"`) || !strings.Contains(body, `"Age":37`) {
		t.Fatalf("body = %s", body)
	}
}

func TestHubReplayDeliversBackfill(t *testing.T) {
	h := sugaar.NewHub(nil)
	defer h.Close()
	h.EnableReplay(3)

	for i := 0; i < 5; i++ {
		h.Publish(sugaar.Event{Topic: "t", Type: "tick"})
	}
	s, c2 := h.Subscribe("t", 8)
	defer c2()
	got := 0
	timeout := time.After(time.Second)
	for got < 3 {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("channel closed early")
			}
			if ev.Type != "tick" {
				t.Fatalf("type = %q", ev.Type)
			}
			got++
		case <-timeout:
			t.Fatalf("only got %d replayed events", got)
		}
	}
}

func TestClientIPHonorsXFF(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.GET("/ip", func(c *sugaar.Context) error { return c.String(200, c.ClientIP()) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ip", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	app.ServeHTTP(rec, req)
	if rec.Body.String() != "203.0.113.5" {
		t.Fatalf("ip = %q", rec.Body.String())
	}
}
