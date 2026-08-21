// Package web holds Forge's static assets, embedded into the binary so a built
// ecs-db is self-contained and Forge works with no network access.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var embedded embed.FS

// Static is the served asset tree, rooted so that the file at
// "static/css/forge.css" is addressable as "css/forge.css".
var Static = mustSub(embedded, "static")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		// dir is a compile-time constant matching the //go:embed directive, so
		// this cannot fail at runtime — only via a bad edit, which should be
		// loud and immediate.
		panic("forge/web: embedding " + dir + ": " + err.Error())
	}
	return sub
}
