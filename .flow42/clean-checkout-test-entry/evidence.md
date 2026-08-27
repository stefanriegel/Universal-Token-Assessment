# Evidence: Make the test entry deterministic from a clean checkout

## Baseline (red)

- Timestamp: 2026-08-27 (Europe/Berlin)
- Environment: clean checkout of `main` at `3cae45dcffcd1dd950d0e414ea6d7494c8a67c20`; `frontend/dist` absent and ignored by `.gitignore`.
- Command: `go test . -count=1`
- Expected: the root package compiles and its tests run without requiring generated frontend output.
- Actual: exit code 1 before tests execute.

```text
# github.com/stefanriegel/Universal-Token-Assessment
embed.go:5:12: pattern all:frontend/dist: no matching files found
FAIL github.com/stefanriegel/Universal-Token-Assessment [setup failed]
FAIL
```

## Repository evidence

- `embed.go:5` declares `//go:embed all:frontend/dist`.
- `.gitignore:33` ignores `frontend/dist/`; the directory is absent in the clean checkout.
- `README.md` documents the frontend build as required for the Go binary, then separately documents `go test ./... -count=1`.
- `Makefile` defines `test` as `go test ./...` and `clean` removes `frontend/dist`.
- `server/server_test.go` states that tests inject static files so they do not require `frontend/dist` at test time, but root-package compilation still requires the directory.

## Independent observation

A partial broader `go test ./... -count=1` run also reported missing `../../testdata/nios-metric-fields.json` and `../../testdata/cross-source-fixture.json` from `internal/exporter`. This is not caused by the embed failure and is outside this intent; it must remain independently visible.

## Known gaps

- No implementation or green evidence exists yet.
- Production bundle verification is deferred until after approved intent, specification, plan, and implementation.

## Approval provenance read-back

- Authenticated `gh` read-back verified owner `stefanriegel` authored configuration comment `5445297002`; its unchanged body bound `.flow42/config.yml` to SHA-256 `98fc829bca648808931218c9861220496a86ad17033c2287509bb6b9915d6a5f` at `2026-08-27T21:13:52Z`.
- The approved configuration bytes initially matched that digest. Current Flow42 preflight then identified scalar `commands.*` values as invalid; owner comment `5445319247` superseded the earlier approval after migration to direct argv arrays.
- Authenticated `gh` read-back verified the superseding owner comment and the current `.flow42/config.yml` SHA-256 `676313c6e72ede3dbae856d01b6f1ab223e8a3779d0d2a5bd1e087ae12ccded5`.
- Authenticated `gh` read-back verified owner `stefanriegel` authored intent comment `5445297262`; its unchanged body bound `intent.md` to SHA-256 `55f75d46ff98c2a320bd4e81702677da81375cb1e4e3c1b04a4935a646fc7329` at `2026-08-27T21:13:53Z`.
- Fresh local hashing matched the approved intent digest before transition to `drafting-spec`.

## Specification gate

- Specification SHA-256: `916755d8aa6c7403dbd57c7f50652cacd9cb2c17dc783fa71d58c78f1844990b`
- The specification separates a deterministic repository-owned test filesystem from an explicit fail-closed production embedding path and requires positive and negative production-bundle proof.
- No product implementation or test command was run in this phase.

## Specification approval read-back

- Authenticated `gh api` read-back verified owner `stefanriegel` authored comment `5445355881` at `2026-08-27T21:19:40Z`, approving the exact specification SHA-256 `916755d8aa6c7403dbd57c7f50652cacd9cb2c17dc783fa71d58c78f1844990b` for planning only.
- Fresh local `shasum -a 256` output matched the approved digest before the transition through planning to building.

## Implementation and verification

### Test-first red

- Added `TestDefaultEmbedUsesRepositoryTestFilesystem`, then ran `go test . -count=1` before changing the embedding implementation.
- Result: exit 1 at package setup with `embed.go:5:12: pattern all:frontend/dist: no matching files found`; the new test could not execute, preserving the original red condition.

### Clean-checkout green and network-free proof

- Implemented a committed minimal fixture below `testdata/static/frontend/dist`, selected for untagged test compilation; untagged executables refuse to start before binding a listener.
- Command: `GOPROXY=off GOSUMDB=off go test . -count=1`.
- Result: exit 0 (`ok ... 0.467s` initially; `0.381s` after final cleanup), with `frontend/dist` absent before and after and an unchanged Git-status snapshot during the first green run.
- Focused command: `go test ./server -run 'TestStaticServing|TestStaticAssets' -count=1`; result: exit 0.
- `make test` now invokes the configured `go test ./... -count=1` command and does not run frontend tooling.

### Fail-closed and positive production proof

- With `frontend/dist` absent, `GOPROXY=off GOSUMDB=off go build -tags=production ...` exited 1 with `embed_production.go:7:12: pattern all:frontend/dist: no matching files found` both before and after positive-build cleanup.
- Added executable-level regression coverage: `TestUntaggedExecutableFailsBeforeServing` builds and runs an ordinary untagged binary, requires a non-zero exit with the test-only diagnostic, and rejects any `listening on` evidence. `TestProductionEmbedUsesRealFrontendBundle` requires production provenance and rejects the fixture marker.
- An offline `pnpm install --frozen-lockfile` attempt failed only because `yaml-1.10.3.tgz` was not cached; a normal locked install then completed, the Vite production build generated `index.html` plus hashed assets, and `go build -tags=production` succeeded.
- Independent UTA remediation re-ran the locked install entirely from the local pnpm cache, built the real Vite bundle, passed `go test -tags=production . -run TestProductionEmbedUsesRealFrontendBundle -count=1`, and produced a real production-tagged binary.
- The resulting binary served the real built `index.html` (687 bytes) and its referenced `assets/index-BQWxPXkj.js` (785818 bytes) over a loopback ephemeral port.
- `Makefile`, Dockerfile, and both GoReleaser configurations explicitly select `production`; `README.md` documents the same requirement. GoReleaser configuration validation was not locally available because the `goreleaser` executable is not installed.

### Broader known failures

- `GOPROXY=off GOSUMDB=off go test ./... -count=1` and equivalent `make test` reached all package tests without the root embed failure but exited 1 on repository-absent fixtures.
- Independent failures include `testdata/nios-metric-fields.json`, `testdata/cross-source-fixture.json`, and the `testdata/ms-allocation/*.{xml,json}` family, affecting `internal/exporter`, `internal/scanner/nios`, and `server`. None was modified or hidden.

### Final residue

- Generated `frontend/dist`, `frontend/node_modules`, and temporary binaries were moved out of the worktree after positive verification.
- Final checks: `frontend/dist` absent, `frontend/node_modules` absent, `git diff --check` clean, and only approved implementation plus `.flow42` lifecycle files modified.
- No commit, branch push, pull request, merge, release, deployment, publication, or other external lifecycle action was performed. Commit, branch push, and pull-request creation are reversible; the irreversible-action gate applies to merge, release, deployment, publication, and comparable irreversible actions.
