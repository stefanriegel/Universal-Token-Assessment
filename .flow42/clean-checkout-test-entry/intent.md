# Intent: Make the test entry deterministic from a clean checkout

- Work ID: `clean-checkout-test-entry`
- Issue: https://github.com/stefanriegel/Universal-Token-Assessment/issues/10
- Status: awaiting intent approval
- Risk: low

## Problem

The repository documents `go test ./... -count=1` and exposes `make test`, but a clean checkout cannot compile the root package. `embed.go` requires `frontend/dist`, while that generated directory is ignored and absent until a frontend build has run. The test entry therefore fails before root tests execute and relies on an undocumented generated-asset precondition.

## Desired outcome

Make Go test compilation deterministic in a clean checkout without weakening the production requirement to embed the built frontend. The canonical test entry and developer documentation should express the same behavior and prerequisites.

## Users

- Contributors running the documented test command after cloning.
- CI or automation validating the repository without preserved build artifacts.
- Maintainers who need production binaries to retain the embedded frontend.

## Constraints

- Keep production frontend embedding intact.
- Do not commit `frontend/dist` or other generated build output.
- Do not require network access merely to compile or run Go tests.
- Preserve the existing server test seam that accepts an injected static filesystem.
- Keep the change narrowly focused on the missing-`frontend/dist` test-entry failure.

## Non-goals

- Redesigning the frontend build, application server, or release pipeline.
- Changing UI behavior or generated frontend contents.
- Fixing the independently observed missing repository-root `testdata` fixtures in `internal/exporter`; record that evidence for separate triage.
- Opening a pull request, merging, publishing, or deploying in this intent stage.

## Acceptance signals

- From a clean checkout, `go test . -count=1` no longer fails with `pattern all:frontend/dist: no matching files found`.
- `go test ./... -count=1` reaches package tests without the root embed setup failure; any independent package failures remain separately attributable.
- The production build path still embeds real files built under `frontend/dist`.
- A regression check exercises the clean-checkout/no-generated-dist condition.
- `README.md`, `Makefile`, and the implemented test entry do not contradict one another.

## Assumptions and risks

- Assumption: test binaries do not need the production frontend bundle because server tests already inject a test filesystem.
- Risk: build tags or alternate embed files could diverge across normal builds, tests, and release builds; verification must cover both test compilation and a production frontend-plus-Go build.
- Risk: making a placeholder asset available to tests could accidentally leak into production; the implementation must make that impossible or prove the production artifact contains the real bundle.
- Known independent gap: `go test ./... -count=1` also reports missing `testdata/nios-metric-fields.json` and `testdata/cross-source-fixture.json` in `internal/exporter`.
