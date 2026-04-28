package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:web/dist
var svelteDist embed.FS

func svelteFS() fs.FS {
	sub, err := fs.Sub(svelteDist, "web/dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// serveSvelte serves the built Svelte SPA. Expects the request path to already
// be stripped of any mount prefix (use http.StripPrefix at the mux). Root path
// serves index.html with no-store so binary updates are picked up on the next
// navigation; hashed asset files under /assets/ are marked immutable.
func serveSvelte() http.Handler {
	root := svelteFS()
	fileSrv := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "" || p == "/" {
			b, err := fs.ReadFile(root, "index.html")
			if err != nil {
				http.Error(w, "svelte build missing", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(b)
			return
		}
		if strings.HasPrefix(p, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileSrv.ServeHTTP(w, r)
	})
}
