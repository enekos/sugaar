package sugaar

import (
	"io/fs"
	"net/http"
	"strings"
)

// Static serves files from dir under urlPrefix. The prefix must end without
// a trailing slash; the route registers as a prefix match (Go 1.22 syntax).
//
//	app.Static("/assets", "./public")
func (a *App) Static(urlPrefix, dir string) {
	prefix := cleanPrefix(urlPrefix)
	fileServer := http.StripPrefix(prefix, http.FileServer(http.Dir(dir)))
	a.Mux.Handle("GET "+prefix+"/", fileServer)
}

// StaticFS serves files from an fs.FS (handy for go:embed assets).
//
//	//go:embed public
//	var assets embed.FS
//	app.StaticFS("/assets", assets, "public")
func (a *App) StaticFS(urlPrefix string, fsys fs.FS, root string) {
	prefix := cleanPrefix(urlPrefix)
	sub, err := fs.Sub(fsys, strings.TrimLeft(root, "./"))
	if err != nil {
		sub = fsys
	}
	fileServer := http.StripPrefix(prefix, http.FileServer(http.FS(sub)))
	a.Mux.Handle("GET "+prefix+"/", fileServer)
}
