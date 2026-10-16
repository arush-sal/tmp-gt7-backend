// Package web embeds the built React SPA (web/dist) so rtps can serve the dashboard from the same
// origin as the read API. Build the frontend first with `make web-build`; a fresh checkout only has
// the placeholder dist/.gitkeep, in which case Handler reports that the frontend is not built.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the embedded SPA. If the frontend was not built into dist (no index.html), every
// request gets a 404 with a plain-text hint instead of a directory listing.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil { // unreachable: "dist" is a valid, embedded path
		panic(err)
	}
	return handlerFor(sub)
}

// handlerFor picks the handler once, at construction time, based on whether fsys has an index.html.
func handlerFor(fsys fs.FS) http.Handler {
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "frontend not built: run `make web-build`", http.StatusNotFound)
		})
	}
	return http.FileServerFS(fsys)
}
