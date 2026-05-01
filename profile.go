package sugaar

import (
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
)

// mountPprof exposes the standard pprof endpoints under /debug/pprof.
//
// Access is gated by Options.PprofAuth when set; otherwise pprof is
// restricted to loopback callers (127.0.0.1, ::1, unix sockets) so a
// publicly-bound server doesn't leak heap dumps to the internet. Toggle
// with Options.DisablePprof.
func (a *App) mountPprof() {
	gate := a.pprofGate()
	register := func(pattern string, h http.HandlerFunc) {
		a.Mux.Handle(pattern, gate(h))
	}
	register("GET /debug/pprof/", pprof.Index)
	register("GET /debug/pprof/cmdline", pprof.Cmdline)
	register("GET /debug/pprof/profile", pprof.Profile)
	register("GET /debug/pprof/symbol", pprof.Symbol)
	register("POST /debug/pprof/symbol", pprof.Symbol)
	register("GET /debug/pprof/trace", pprof.Trace)
	for _, name := range []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"} {
		register("GET /debug/pprof/"+name, pprof.Handler(name).ServeHTTP)
	}
}

// pprofGate returns a middleware factory enforcing the configured pprof
// access policy (Authenticator override or loopback-only).
func (a *App) pprofGate() func(http.HandlerFunc) http.Handler {
	if auth := a.opts.PprofAuth; auth != nil {
		mw := Auth(auth)
		return func(h http.HandlerFunc) http.Handler {
			return a.adapt(mw(func(c *Context) error { h(c.W(), c.R()); return nil }))
		}
	}
	return func(h http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isLoopback(r.RemoteAddr) {
				http.Error(w, "pprof restricted to loopback (set Options.PprofAuth to expose)", http.StatusForbidden)
				return
			}
			h(w, r)
		})
	}
}

// adapt converts a sugaar HandlerFunc into an http.Handler so pprof routes
// can flow through Auth middleware that expects a *Context.
func (a *App) adapt(h HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c *Context
		reqCtxMu.Lock()
		c = reqCtxMap[r]
		reqCtxMu.Unlock()
		if c != nil {
			c.w = w
			c.r = r
		} else {
			c = contextPool.Get().(*Context)
			c.app = a
			c.reset(w, r)
			defer func() { c.app = nil; c.reset(nil, nil); contextPool.Put(c) }()
		}
		if err := h(c); err != nil {
			a.opts.ErrorHandler(c, err)
		}
	})
}

func isLoopback(remoteAddr string) bool {
	host := remoteAddr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		// Unix sockets and similar appear with empty host.
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
