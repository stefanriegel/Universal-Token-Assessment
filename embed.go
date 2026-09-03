//go:build !production

package main

import (
	"io/fs"
	"testing/fstest"
	"time"
)

// devPlaceholderMarker identifies the synthetic bundle served by untagged
// builds. Production builds embed the real frontend and must never contain it.
const devPlaceholderMarker = "uta-dev-placeholder-static-files"

// staticFiles is a synthetic, in-memory stand-in for the production frontend
// bundle. Untagged builds are test-only and refuse to start (see
// requireProductionBuild), so they carry no real assets and nothing is embedded
// from disk.
var staticFiles fs.FS = fstest.MapFS{
	"frontend/dist/index.html": &fstest.MapFile{
		Data: []byte(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>` + devPlaceholderMarker + `</title></head>
<body>` + devPlaceholderMarker + `</body>
</html>
`),
		Mode:    0o444,
		ModTime: time.Time{},
	},
}

const productionBuild = false
