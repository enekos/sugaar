package sugaar

import "net/http/pprof"

// mountPprof exposes the standard pprof endpoints under /debug/pprof.
// Toggle with Options.DisablePprof.
func (a *App) mountPprof() {
	a.Mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	a.Mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	a.Mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	a.Mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	a.Mux.HandleFunc("POST /debug/pprof/symbol", pprof.Symbol)
	a.Mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	for _, name := range []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"} {
		a.Mux.Handle("GET /debug/pprof/"+name, pprof.Handler(name))
	}
}
