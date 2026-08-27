VERSION := $(shell git describe --tags --long --always 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w -X github.com/stefanriegel/Universal-Token-Assessment/internal/version.Version=$(VERSION) -X github.com/stefanriegel/Universal-Token-Assessment/internal/version.Commit=$(COMMIT)

.PHONY: build build-frontend build-go build-windows test clean verify-vnios-specs verify-privacy verify-privacy-selftest

# build: default local build for the current host (Mac/Linux). Use
# `make build-windows` for the cross-compiled Windows .exe.
build: build-frontend build-go

build-frontend:
	cd frontend && pnpm install && pnpm build

build-go:
	go build -tags=production -ldflags="$(LDFLAGS)" -o Universal-Token-Assessment .

# build-windows: dev-only Windows cross-compile from macOS/Linux. CGO=0 keeps
# the build hermetic (no mingw toolchain required) — SSPI (Windows SSO) is
# stubbed via the `!cgo` build tag in internal/scanner/ad/sspi_stub.go.
# Production Windows builds run via GoReleaser with CGO=1 + mingw-w64 (see
# .goreleaser*.yaml) so SSPI works at runtime.
build-windows: build-frontend
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags=production -ldflags="$(LDFLAGS)" -o Universal-Token-Assessment.exe .

test:
	go test ./... -count=1

clean:
	rm -f Universal-Token-Assessment Universal-Token-Assessment.exe
	rm -rf frontend/dist

# verify-vnios-specs — VNIOS_SPECS drift detector. Compares Go and TS canonical
# JSON byte-for-byte and verifies both match the committed hash file. Exits
# non-zero with a side-by-side row diff on mismatch. Wired into CI.
verify-vnios-specs:
	@go run ./cmd/verify-vnios-specs

# verify-privacy — MSPAR-05 static half (D-13/D-14/D-15). Enforces a positive
# reserved-range address/domain rule over the Microsoft-allocation surface
# plus testdata/, and reuses .githooks/pre-commit's customer-name denylist in
# tree mode. Exits non-zero with file:line on any violation.
verify-privacy:
	@bash scripts/privacy-scan.sh

# verify-privacy-selftest — the proving run for verify-privacy. Plants
# synthetic values on top of the real scan and asserts the scan catches them,
# so a regression that makes the gate structurally blind fails a build
# instead of only ever being observed passing.
verify-privacy-selftest:
	@bash scripts/privacy-scan-selftest.sh
