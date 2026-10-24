// Package panel embeds the built browser panel (web/ -> nuxt generate).
// `task build` copies web/.output/public here before compiling; without it
// the binary serves a short notice instead of the panel.
package panel

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built panel files, rooted at index.html.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// The embed directive guarantees dist exists.
		panic(err)
	}
	return sub
}
