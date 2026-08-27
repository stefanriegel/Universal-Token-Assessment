//go:build !production

package main

import (
	"io/fs"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/server"
)

func TestDefaultEmbedUsesRepositoryTestFilesystem(t *testing.T) {
	contents, err := fs.ReadFile(staticFiles, "frontend/dist/index.html")
	if err != nil {
		t.Fatalf("read test index: %v", err)
	}
	if !strings.Contains(string(contents), "flow42-test-static-files") {
		t.Fatal("default embed does not contain the repository test fixture marker")
	}
	if _, err := server.NewStaticHandler(staticFiles); err != nil {
		t.Fatalf("create static handler from test filesystem: %v", err)
	}
}

func TestDefaultBuildFailsClosed(t *testing.T) {
	err := requireProductionBuild()
	if err == nil {
		t.Fatal("untagged build was allowed to start")
	}
	if !strings.Contains(err.Error(), "test-only") || !strings.Contains(err.Error(), "-tags=production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUntaggedExecutableFailsBeforeServing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable suffix and process behavior are covered on Unix CI")
	}

	binary := filepath.Join(t.TempDir(), "uta")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build untagged executable: %v\n%s", err, output)
	}

	run := exec.Command(binary)
	output, err := run.CombinedOutput()
	if err == nil {
		t.Fatal("untagged executable started successfully")
	}
	message := string(output)
	if !strings.Contains(message, "refusing to start test-only build") {
		t.Fatalf("unexpected executable output: %s", message)
	}
	if strings.Contains(message, "listening on") {
		t.Fatalf("untagged executable bound a listener before failing: %s", message)
	}
}
