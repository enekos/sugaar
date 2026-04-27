// Package sugaar is a minimal Go web framework optimised for streaming
// agentic events over WebSockets and SSE.
//
// The core is built on the standard library's http.ServeMux (Go 1.22+ pattern
// router), wrapped with a typed Context, a middleware chain, and an event
// Hub. There are no third-party router or context dependencies; gorilla
// websocket and x/crypto/acme/autocert are used for what stdlib doesn't
// cover.
//
// Design goals:
//
//   - One binary, zero config to start. Sensible defaults; sane logs.
//   - First-class WebSocket and SSE for agentic event streaming.
//   - HTTPS in production via Let's Encrypt autocert; static-cert fallback.
//   - pprof always available; you can profile the live VPS.
//   - Tests are "approved truth": diff readable golden files in plain text.
package sugaar

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

// Options configures an App. The zero value runs an HTTP server on :8080
// with pprof enabled.
type Options struct {
	Addr             string         // plain HTTP listen addr; default ":8080"
	TLSAddr          string         // HTTPS listen addr; default ":8443" or ":443" with autocert
	AutoCertDomains  []string       // when set, enables Let's Encrypt
	AutoCertCacheDir string         // default "./certs"
	CertFile, KeyFile string        // static cert/key (alternative to autocert)
	DisablePprof     bool           // pprof is mounted by default
	Logger           *slog.Logger   // default slog.Default()
	ShutdownTimeout  time.Duration  // default 15s
	ErrorHandler     ErrorHandler   // override default error response
}

// ErrorHandler converts a HandlerFunc error into an HTTP response. The
// default writes a 500 with a small JSON body.
type ErrorHandler func(c *Context, err error)

func (o *Options) defaults() {
	if o.Addr == "" {
		o.Addr = ":8080"
	}
	if o.TLSAddr == "" {
		if len(o.AutoCertDomains) > 0 {
			o.TLSAddr = ":443"
		} else {
			o.TLSAddr = ":8443"
		}
	}
	if o.AutoCertCacheDir == "" {
		o.AutoCertCacheDir = "./certs"
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = 15 * time.Second
	}
	if o.ErrorHandler == nil {
		o.ErrorHandler = defaultErrorHandler
	}
}

// App is a sugaar application. Safe for concurrent use after New returns.
//
// The mux is exposed for advanced users who want raw http.HandlerFunc
// registration; everyday code should use App.GET, App.POST, etc.
type App struct {
	Mux *http.ServeMux
	Hub *Hub

	opts Options
	log  *slog.Logger
	mws  []Middleware
	grpc *GRPC
}

// New constructs an App.
func New(opts Options) *App {
	opts.defaults()
	a := &App{
		Mux:  http.NewServeMux(),
		Hub:  NewHub(opts.Logger),
		opts: opts,
		log:  opts.Logger,
	}
	a.Use(recoverMiddleware(a.log), requestLogMiddleware(a.log))
	if !opts.DisablePprof {
		a.mountPprof()
	}
	return a
}

// Use appends middleware. Order matters: first added runs outermost.
func (a *App) Use(mw ...Middleware) { a.mws = append(a.mws, mw...) }

// Handle registers h for "METHOD /pattern". Pattern follows Go 1.22 syntax,
// e.g. "GET /users/{id}" or "/static/" for prefix routes. App-level
// middleware (App.Use) wraps the entire mux and runs first, so it sees
// requests even when a method isn't routed; group/route middleware runs only
// for matched routes.
func (a *App) Handle(pattern string, h HandlerFunc) {
	a.Mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		c, _ := r.Context().Value(ctxKey{}).(*Context)
		if c == nil {
			// Direct mux invocation (e.g. tests bypassing ServeHTTP).
			c = contextPool.Get().(*Context)
			c.app = a
			c.reset(w, r)
			defer func() {
				c.app = nil
				c.reset(nil, nil)
				contextPool.Put(c)
			}()
		} else {
			// Use the already-wrapped writer from app middleware.
			c.w = w
			c.r = r
		}
		if err := h(c); err != nil {
			a.opts.ErrorHandler(c, err)
		}
	})
}

// ctxKey identifies the *Context smuggled through r.Context() so route
// handlers reuse the one app middleware already prepared.
type ctxKey struct{}

// GET / POST / PUT / DELETE / PATCH / HEAD / OPTIONS register a method-bound route.
func (a *App) GET(path string, h HandlerFunc)     { a.Handle("GET "+path, h) }
func (a *App) POST(path string, h HandlerFunc)    { a.Handle("POST "+path, h) }
func (a *App) PUT(path string, h HandlerFunc)     { a.Handle("PUT "+path, h) }
func (a *App) DELETE(path string, h HandlerFunc)  { a.Handle("DELETE "+path, h) }
func (a *App) PATCH(path string, h HandlerFunc)   { a.Handle("PATCH "+path, h) }
func (a *App) HEAD(path string, h HandlerFunc)    { a.Handle("HEAD "+path, h) }
func (a *App) OPTIONS(path string, h HandlerFunc) { a.Handle("OPTIONS "+path, h) }

// ServeHTTP makes App an http.Handler. App-level middleware (App.Use) wraps
// the entire mux dispatch, so it observes preflight requests, missing
// routes, and 405s.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c := contextPool.Get().(*Context)
	c.app = a
	c.reset(w, r)
	r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, c))
	c.r = r
	defer func() {
		c.app = nil
		c.reset(nil, nil)
		contextPool.Put(c)
	}()

	h := func(c *Context) error {
		a.Mux.ServeHTTP(c.W(), c.R())
		return nil
	}
	if err := chain(h, a.mws)(c); err != nil {
		a.opts.ErrorHandler(c, err)
	}
}

// chain composes middleware around a handler. Outer middleware wraps inner.
func chain(h HandlerFunc, mws []Middleware) HandlerFunc {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// requestLogMiddleware emits one structured line per request.
func requestLogMiddleware(log *slog.Logger) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: c.w}
			c.w = sw
			err := next(c)
			log.Info("http",
				"method", c.r.Method,
				"path", c.r.URL.Path,
				"status", sw.status,
				"bytes", sw.bytes,
				"dur", time.Since(start),
			)
			return err
		}
	}
}

// recoverMiddleware turns panics into errors so ErrorHandler can respond.
func recoverMiddleware(log *slog.Logger) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					log.Error("panic", "err", r, "stack", string(debug.Stack()))
					err = fmt.Errorf("panic: %v", r)
				}
			}()
			return next(c)
		}
	}
}

func defaultErrorHandler(c *Context, err error) {
	if he, ok := asHTTPError(err); ok {
		_ = c.JSON(he.Status, he)
		return
	}
	_ = c.JSON(http.StatusInternalServerError, map[string]string{"error": sanitize(err.Error())})
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// statusWriter records status and bytes written for the access log.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Flush exposes the underlying flusher for SSE.
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack exposes the underlying hijacker for WebSocket upgrades.
func (s *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := s.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Unwrap exposes the wrapped writer so http.NewResponseController can
// reach SetWriteDeadline / SetReadDeadline on the real connection.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Run starts the server(s) and blocks until ctx (or SIGINT/SIGTERM) cancels.
// When EnableGRPC has been called, the gRPC server runs alongside HTTP and
// shares the lifecycle.
func (a *App) Run(ctx context.Context) error {
	if ctx == nil {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
	}

	grpcErrCh, stopGRPC, err := a.runGRPC(ctx)
	if err != nil {
		return err
	}
	defer stopGRPC()

	httpErrCh := make(chan error, 1)
	go func() {
		switch {
		case len(a.opts.AutoCertDomains) > 0:
			httpErrCh <- a.runAutoCert(ctx)
		case a.opts.CertFile != "" && a.opts.KeyFile != "":
			httpErrCh <- a.runStaticTLS(ctx)
		default:
			httpErrCh <- a.runPlain(ctx)
		}
	}()

	select {
	case <-ctx.Done():
		return <-httpErrCh
	case err := <-httpErrCh:
		return err
	case err := <-grpcErrCh:
		if err != nil {
			a.log.Error("sugaar: gRPC failed", "err", err)
		}
		return err
	}
}

func (a *App) runPlain(ctx context.Context) error {
	srv := &http.Server{Addr: a.opts.Addr, Handler: a}
	a.log.Info("sugaar: serving HTTP", "addr", a.opts.Addr)
	return serveAndShutdown(ctx, a.opts.ShutdownTimeout, srv, srv.ListenAndServe)
}

func (a *App) runStaticTLS(ctx context.Context) error {
	srv := &http.Server{Addr: a.opts.TLSAddr, Handler: a}
	a.log.Info("sugaar: serving HTTPS", "addr", a.opts.TLSAddr)
	return serveAndShutdown(ctx, a.opts.ShutdownTimeout, srv, func() error {
		return srv.ListenAndServeTLS(a.opts.CertFile, a.opts.KeyFile)
	})
}

func (a *App) runAutoCert(ctx context.Context) error {
	if err := os.MkdirAll(a.opts.AutoCertCacheDir, 0o700); err != nil {
		return fmt.Errorf("autocert cache: %w", err)
	}
	m := &autocert.Manager{
		Cache:      autocert.DirCache(a.opts.AutoCertCacheDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(a.opts.AutoCertDomains...),
	}
	httpsSrv := &http.Server{
		Addr:      a.opts.TLSAddr,
		Handler:   a,
		TLSConfig: &tls.Config{GetCertificate: m.GetCertificate, MinVersion: tls.VersionTLS12},
	}
	httpSrv := &http.Server{
		Addr:    a.opts.Addr,
		Handler: m.HTTPHandler(http.HandlerFunc(redirectHTTPS)),
	}
	a.log.Info("sugaar: autocert HTTPS", "addr", a.opts.TLSAddr, "domains", a.opts.AutoCertDomains)

	errCh := make(chan error, 2)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	go func() { errCh <- httpsSrv.ListenAndServeTLS("", "") }()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.opts.ShutdownTimeout)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	return httpsSrv.Shutdown(shutdownCtx)
}

func redirectHTTPS(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusMovedPermanently)
}

func serveAndShutdown(ctx context.Context, timeout time.Duration, srv *http.Server, listen func() error) error {
	errCh := make(chan error, 1)
	go func() { errCh <- listen() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
