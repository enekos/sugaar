package sugaar

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
)

// HandlerFunc is sugaar's handler signature. Returning a non-nil error lets
// the surrounding error middleware (or App.ErrorHandler) decide the response.
type HandlerFunc func(c *Context) error

// Middleware wraps a HandlerFunc with cross-cutting behavior.
// Compose with App.Use; first registered runs outermost.
type Middleware func(HandlerFunc) HandlerFunc

// Context is the per-request handle. It is pooled, so do NOT keep references
// past the handler return. Use Context.Request().Context() for cancellation.
type Context struct {
	w   http.ResponseWriter
	r   *http.Request
	app *App
	sw  statusWriter // embedded for reuse by requestLogMiddleware

	// store is a tiny per-request map; nil until first Set.
	store map[string]any
}

// reset prepares a pooled Context for reuse.
func (c *Context) reset(w http.ResponseWriter, r *http.Request) {
	c.w = w
	c.r = r
	c.store = nil
	c.sw = statusWriter{}
}

// W returns the underlying ResponseWriter.
func (c *Context) W() http.ResponseWriter { return c.w }

// R returns the underlying *http.Request.
func (c *Context) R() *http.Request { return c.r }

// Ctx returns the request's context.Context.
func (c *Context) Ctx() context.Context { return c.r.Context() }

// Param returns a path parameter declared in the route pattern, e.g. "{id}".
// Backed by Go 1.22's request.PathValue, so it's allocation-free.
func (c *Context) Param(name string) string { return c.r.PathValue(name) }

// Query returns a URL query parameter.
func (c *Context) Query(name string) string { return c.r.URL.Query().Get(name) }

// QueryInt parses a query parameter as int with a fallback.
func (c *Context) QueryInt(name string, def int) int {
	v := c.Query(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// Header returns a request header.
func (c *Context) Header(name string) string { return c.r.Header.Get(name) }

// Set stores a value on the context. Used by middleware to pass data downstream.
func (c *Context) Set(key string, val any) {
	if c.store == nil {
		c.store = make(map[string]any, 4)
	}
	c.store[key] = val
}

// Get retrieves a value previously stored with Set.
func (c *Context) Get(key string) (any, bool) {
	v, ok := c.store[key]
	return v, ok
}

// BindJSON decodes the request body as JSON into dst. The body is capped by
// Options.MaxBodyBytes (default 1 MiB) and closed when the call returns.
// On overflow it returns 413 Payload Too Large; on malformed JSON, 400.
func (c *Context) BindJSON(dst any) error {
	if c.r.Body == nil {
		return BadRequest("empty body")
	}
	body := c.limitedBody()
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return httpErr(http.StatusRequestEntityTooLarge, "request body exceeds limit")
		}
		return BadRequest(err.Error()).WithCause(err)
	}
	return nil
}

// limitedBody wraps r.Body in http.MaxBytesReader using the App's configured
// body size cap. A negative cap disables the limit; a nil app falls back to
// the original body so tests bypassing New still work.
func (c *Context) limitedBody() io.ReadCloser {
	if c.app == nil || c.app.opts.MaxBodyBytes < 0 {
		return c.r.Body
	}
	return http.MaxBytesReader(c.w, c.r.Body, c.app.opts.MaxBodyBytes)
}

// JSON writes status and a JSON-encoded body. The Content-Type is set
// automatically.
func (c *Context) JSON(status int, body any) error {
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.w.WriteHeader(status)
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = c.w.Write(data)
	return err
}

// String writes a plain-text response.
func (c *Context) String(status int, s string) error {
	c.w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.w.WriteHeader(status)
	_, err := io.WriteString(c.w, s)
	return err
}

// Status writes a header-only response.
func (c *Context) Status(code int) error {
	c.w.WriteHeader(code)
	return nil
}

// contextPool keeps Context allocations off the hot path.
var contextPool = sync.Pool{
	New: func() any { return new(Context) },
}
