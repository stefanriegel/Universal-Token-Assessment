//go:build production

package main

import (
	"io/fs"
	"strings"
	"testing"
)

func TestProductionEmbedUsesRealFrontendBundle(t *testing.T) {
	if err := requireProductionBuild(); err != nil {
		t.Fatalf("production build rejected: %v", err)
	}
	contents, err := fs.ReadFile(staticFiles, "frontend/dist/index.html")
	if err != nil {
		t.Fatalf("read production index: %v", err)
	}
	if strings.Contains(string(contents), "flow42-test-static-files") {
		t.Fatal("production embed contains the repository test fixture")
	}
}
