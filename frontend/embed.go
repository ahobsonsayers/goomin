package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS is the built app, rooted at index.html.
func FS() (fs.FS, error) { return fs.Sub(distFS, "dist") }
