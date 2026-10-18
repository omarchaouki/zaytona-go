// Package web embeds the built front-end assets (web/static/dist, produced by
// tools/assetbuild). "all:" keeps .gitkeep so a fresh clone still compiles.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:static/dist
var dist embed.FS

// Dist returns the built assets rooted at web/static/dist.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "static/dist")
	if err != nil {
		panic(err)
	}
	return sub
}
