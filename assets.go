package main

import (
	"embed"
	"io/fs"
)

// Build the React app with npm run build before running or building Go.
//
//go:embed all:dist
var frontendFiles embed.FS

func frontendFS() fs.FS {
	files, err := fs.Sub(frontendFiles, "dist")
	if err != nil {
		panic(err)
	}
	return files
}
