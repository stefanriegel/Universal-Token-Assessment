# Specification: Make the test entry deterministic from a clean checkout

## Functional requirements

1. The root package must compile under `go test . -count=1` when `frontend/dist` does not exist.
2. The repository-wide test entry must invoke Go tests without first installing frontend dependencies, building the frontend, downloading test-only assets, or creating `frontend/dist` in the checkout.
3. Test compilation may use a deterministic, repository-owned static-files fixture, but that fixture must be distinct from the production frontend bundle and sufficient only to satisfy the root package's static-files interface.
4. Production build entries must continue to build `frontend/dist` and embed the real contents of that directory in the application binary. They must fail closed when the real bundle is absent or invalid; they must not fall back to the test fixture.
5. `make build`, `make build-go`, `make build-windows`, Docker builds, and GoReleaser builds must all select the production embedding path. Any build constraint or generated input used to separate test and production paths must be explicit in each applicable entry.
6. The application must continue to pass an embedded filesystem to `server.NewStaticHandler`, with production files addressable below `frontend/dist`; routing and UI behavior are unchanged.
7. The change must not add generated frontend output to version control or modify the independently missing `internal/exporter` fixtures.
8. `README.md`, `Makefile`, and build/release configuration must document or encode the same test and production prerequisites.

## Non-functional requirements

- A clean-checkout Go test run must be network-free after existing Go module dependencies are available; it must not invoke `pnpm`, a package registry, or a frontend build.
- Test setup must not write ignored artifacts into the checkout and must leave `frontend/dist` absent when it began absent.
- The production binary must remain a self-contained binary serving the built frontend.
- The solution must be narrow, deterministic across supported local and CI build entries, and use standard Go build/embed behavior plus existing repository tooling.
- A missing or accidentally selected production asset set must produce an actionable build or verification failure rather than a runnable binary containing test content.

## Domain model and terminology

- **Production bundle**: the real frontend output generated under `frontend/dist` and embedded into distributable binaries.
- **Test fixture**: a small, committed, repository-owned filesystem used only while compiling or testing without the production bundle.
- **Production embedding path**: the source and build configuration that select and embed the production bundle.
- **Test entry**: the documented `go test ./... -count=1` command and the equivalent `make test` target.
- **Clean checkout**: a checkout of tracked files with no ignored/generated `frontend/dist` directory.

## Interfaces and data

- The root package retains a `staticFiles` value compatible with `fs.FS` / `embed.FS` for `server.NewStaticHandler`.
- `server.NewStaticHandler` and its expected `frontend/dist` subtree remain unchanged unless a strictly equivalent path adaptation is required and covered by tests.
- `make test` is the canonical repository test wrapper and must remain semantically equivalent to the documented repository-wide Go test command.
- Production Make, Docker, and GoReleaser entries are packaging interfaces: each must select the real-bundle path and must never select the test fixture.
- No runtime API, persisted data, environment variable, credential, or network protocol changes are introduced.

## Security considerations

The relevant trust boundary is build provenance: test-only content must not cross into a production binary. The primary threat is an apparently successful release build that silently packages a placeholder fixture. Controls are explicit production-path selection, fail-closed absence checks, and verification of known real frontend assets inside the built binary or its served output. Tests must not add a new dependency-download or script-execution path, and external frontend content must not be fetched during Go testing.

## Acceptance criteria

1. In a clean checkout where `frontend/dist` is absent, `go test . -count=1` exits successfully and does not report `pattern all:frontend/dist: no matching files found`.
2. In the same condition, the configured repository-wide test command reaches package tests without the root embed setup failure, performs no frontend install/build, and does not create `frontend/dist`.
3. `make test` invokes behavior equivalent to the configured repository-wide test command and requires no frontend prerequisites.
4. A production build starting after the normal frontend build embeds and serves identifiable real bundle assets, including the built entry document and at least one generated asset referenced by it.
5. A production-path build with the real bundle deliberately absent fails; no test fixture can satisfy that check.
6. All production build/release entry configurations select the production embedding path, verified by static inspection and at least one representative local production build.
7. Existing server static-handler tests continue to pass without requiring the production bundle.
8. Any `internal/exporter` fixture failures from the broader run are reported as independent failures and are not hidden, relabeled, or changed by this work.
9. After verification, Git status contains no generated `frontend/dist` content or unrelated product changes beyond the approved implementation scope.

## Verification strategy

1. Create an isolated clean checkout at the approved base revision and assert `frontend/dist` is absent before testing.
2. Run `go test . -count=1`, then the configured `go test ./... -count=1`, capturing exit status and distinguishing independent package failures.
3. Record filesystem state before and after test execution to prove that tests neither create `frontend/dist` nor modify tracked files.
4. Use a local network-denial or dependency-warmed environment to prove the test path does not call frontend package installation, registries, or other network services.
5. Run focused static-handler tests with the repository-owned test fixture.
6. Build the frontend through the existing production build flow, build the Go binary through each distinct production configuration class, and verify the binary serves identifiable files from the real generated bundle.
7. Negative-test the production path with `frontend/dist` absent and assert a clear failure before a distributable binary is accepted.
8. Inspect Make, Docker, and GoReleaser configuration to prove they cannot select the test fixture; retain exact commands, outputs, hashes, and any independent failures in `evidence.md`.
