//go:build !production

package main

import (
	"embed"
	"io/fs"
)

//go:embed all:testdata/static/frontend/dist
var testStaticFiles embed.FS

var staticFiles = mustSub(testStaticFiles, "testdata/static")

const productionBuild = false

func mustSub(files fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(files, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
