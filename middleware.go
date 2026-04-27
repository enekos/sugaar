package sugaar

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// requestIDKey is the Context.Set key for the request ID.
const requestIDKey = "sugaar.request_id"

// RequestID assigns a unique ID per request and exposes it on the response
// header (X-Request-Id) and via Context.RequestID. Honors an inbound
// X-Request-Id header so traces propagate.
func RequestID() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			id := c.R().Header.Get("X-Request-Id")
			if id == "" {
				var b [12]byte
				_, _ = rand.Read(b[:])
				id = hex.EncodeToString(b[:])
			}
			c.W().Header().Set("X-Request-Id", id)
			c.Set(requestIDKey, id)
			return next(c)
		}
	}
}

// RequestID returns the request's ID if RequestID middleware is installed.
func (c *Context) RequestID() string {
	v, _ := c.Get(requestIDKey)
	s, _ := v.(string)
	return s
}

// CORSOptions configures the CORS middleware. Zero values fall back to
// permissive defaults suitable for local dev.
type CORSOptions struct {
	AllowOrigins     []string      // exact origins; "*" allowed for dev
	AllowMethods     []string      // default GET,POST,PUT,PATCH,DELETE,OPTIONS
	AllowHeaders     []string      // default Content-Type, Authorization
	AllowCredentials bool
	MaxAge           time.Duration // default 5 minutes
}

// CORS returns a middleware that emits the configured CORS headers and
// short-circuits OPTIONS preflight with 204.
func CORS(o CORSOptions) Middleware {
	if len(o.AllowOrigins) == 0 {
		o.AllowOrigins = []string{"*"}
	}
	if len(o.AllowMethods) == 0 {
		o.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	}
	if len(o.AllowHeaders) == 0 {
		o.AllowHeaders = []string{"Content-Type", "Authorization"}
	}
	if o.MaxAge == 0 {
		o.MaxAge = 5 * time.Minute
	}
	allowMethods := strings.Join(o.AllowMethods, ", ")
	allowHeaders := strings.Join(o.AllowHeaders, ", ")
	maxAge := strconvDuration(o.MaxAge)

	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			origin := c.Header("Origin")
			allow := matchOrigin(origin, o.AllowOrigins)
			if allow != "" {
				h := c.W().Header()
				h.Set("Access-Control-Allow-Origin", allow)
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Methods", allowMethods)
				h.Set("Access-Control-Allow-Headers", allowHeaders)
				if o.AllowCredentials {
					h.Set("Access-Control-Allow-Credentials", "true")
				}
				h.Set("Access-Control-Max-Age", maxAge)
			}
			if c.R().Method == http.MethodOptions {
				return c.Status(http.StatusNoContent)
			}
			return next(c)
		}
	}
}

func matchOrigin(origin string, allow []string) string {
	for _, o := range allow {
		if o == "*" {
			return "*"
		}
		if o == origin {
			return origin
		}
	}
	return ""
}

func strconvDuration(d time.Duration) string {
	// Access-Control-Max-Age is integer seconds.
	return itoa(int(d / time.Second))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		b[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

// Timeout installs a deadline on the request context. Handlers MUST honor
// c.Ctx() to observe cancellation; this middleware does not forcibly stop
// them. If the deadline elapsed by the time the handler returns and no
// response has been written, the client gets 504.
func Timeout(d time.Duration) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			ctx, cancel := context.WithTimeout(c.Ctx(), d)
			defer cancel()
			c.r = c.r.WithContext(ctx)
			err := next(c)
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return httpErr(http.StatusGatewayTimeout, "request timed out")
			}
			return err
		}
	}
}

// BasicAuth gates the chain with HTTP Basic credentials. On success, sets
// "user" on the context.
func BasicAuth(user, pass string) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			u, p, ok := c.R().BasicAuth()
			if !ok || subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 ||
				subtle.ConstantTimeCompare([]byte(p), []byte(pass)) != 1 {
				c.W().Header().Set("WWW-Authenticate", `Basic realm="sugaar"`)
				return Unauthorized("")
			}
			c.Set("user", u)
			return next(c)
		}
	}
}

// BearerAuth requires "Authorization: Bearer <token>" matching one of the
// supplied tokens (constant-time compared).
func BearerAuth(tokens ...string) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			h := c.Header("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(h, prefix) {
				return Unauthorized("missing bearer token")
			}
			got := []byte(h[len(prefix):])
			for _, t := range tokens {
				if subtle.ConstantTimeCompare(got, []byte(t)) == 1 {
					return next(c)
				}
			}
			return Unauthorized("invalid bearer token")
		}
	}
}

// GZip compresses responses with gzip when the client accepts it. Skips
// streaming responses (SSE / chunked WebSocket upgrades), text under 1 KB,
// and already-compressed types.
func GZip() Middleware {
	pool := &sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			if !strings.Contains(c.Header("Accept-Encoding"), "gzip") {
				return next(c)
			}
			gw := &gzipWriter{ResponseWriter: c.W(), pool: pool}
			defer gw.Close()
			c.w = gw
			c.W().Header().Set("Vary", "Accept-Encoding")
			return next(c)
		}
	}
}

type gzipWriter struct {
	http.ResponseWriter
	pool   *sync.Pool
	zw     *gzip.Writer
	wrote  bool
	bypass bool
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.bypass {
		g.ResponseWriter.WriteHeader(code)
		return
	}
	ct := g.Header().Get("Content-Type")
	if shouldSkipGzip(ct) {
		g.bypass = true
		g.ResponseWriter.WriteHeader(code)
		return
	}
	g.Header().Set("Content-Encoding", "gzip")
	g.Header().Del("Content-Length")
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.wrote {
		g.wrote = true
		g.WriteHeader(http.StatusOK)
	}
	if g.bypass {
		return g.ResponseWriter.Write(b)
	}
	if g.zw == nil {
		g.zw = g.pool.Get().(*gzip.Writer)
		g.zw.Reset(g.ResponseWriter)
	}
	return g.zw.Write(b)
}

func (g *gzipWriter) Close() {
	if g.zw != nil {
		_ = g.zw.Close()
		g.pool.Put(g.zw)
		g.zw = nil
	}
}

func shouldSkipGzip(ct string) bool {
	switch {
	case strings.HasPrefix(ct, "text/event-stream"):
		return true
	case strings.HasPrefix(ct, "image/"):
		return true
	case strings.Contains(ct, "zip"), strings.Contains(ct, "gzip"):
		return true
	}
	return false
}

// b64Encode is a small helper kept for symmetry with future signed-cookie work.
var b64 = base64.RawURLEncoding
