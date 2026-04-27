package sugaar

import "strings"

// Group is a sub-router with a path prefix and its own middleware stack.
// Middleware added with Group.Use applies only to routes registered under
// that group; the App-level chain still runs first.
//
//	api := app.Group("/api/v1")
//	api.Use(BearerAuth("secret"))
//	api.GET("/me", handler)
type Group struct {
	app    *App
	prefix string
	mws    []Middleware
}

// Group creates a new sub-router rooted at prefix.
func (a *App) Group(prefix string, mws ...Middleware) *Group {
	return &Group{app: a, prefix: cleanPrefix(prefix), mws: append([]Middleware{}, mws...)}
}

// Group nests another sub-router under g.
func (g *Group) Group(prefix string, mws ...Middleware) *Group {
	combined := append([]Middleware{}, g.mws...)
	combined = append(combined, mws...)
	return &Group{app: g.app, prefix: g.prefix + cleanPrefix(prefix), mws: combined}
}

// Use appends middleware that runs only for handlers registered on this group.
func (g *Group) Use(mws ...Middleware) { g.mws = append(g.mws, mws...) }

// Handle registers "METHOD /path" with both group and app middleware applied.
// Pattern syntax matches App.Handle.
func (g *Group) Handle(pattern string, h HandlerFunc) {
	method, path, ok := splitMethodPath(pattern)
	full := g.prefix + path
	wrapped := chain(h, g.mws)
	if ok {
		g.app.Handle(method+" "+full, wrapped)
	} else {
		g.app.Handle(full, wrapped)
	}
}

func (g *Group) GET(p string, h HandlerFunc)     { g.Handle("GET "+p, h) }
func (g *Group) POST(p string, h HandlerFunc)    { g.Handle("POST "+p, h) }
func (g *Group) PUT(p string, h HandlerFunc)     { g.Handle("PUT "+p, h) }
func (g *Group) DELETE(p string, h HandlerFunc)  { g.Handle("DELETE "+p, h) }
func (g *Group) PATCH(p string, h HandlerFunc)   { g.Handle("PATCH "+p, h) }
func (g *Group) HEAD(p string, h HandlerFunc)    { g.Handle("HEAD "+p, h) }
func (g *Group) OPTIONS(p string, h HandlerFunc) { g.Handle("OPTIONS "+p, h) }

func cleanPrefix(p string) string {
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(p, "/")
}

// splitMethodPath separates "GET /x" into ("GET", "/x", true). Patterns
// without a method return ("", pattern, false).
func splitMethodPath(pattern string) (method, path string, ok bool) {
	i := strings.IndexByte(pattern, ' ')
	if i < 0 {
		return "", pattern, false
	}
	return pattern[:i], pattern[i+1:], true
}
