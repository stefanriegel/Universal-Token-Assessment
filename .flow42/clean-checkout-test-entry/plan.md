# Plan: Make the test entry deterministic from a clean checkout

## Vertical slices

### Slice 1: Separate test and production embedding

- Outcome: clean-checkout tests use a committed minimal filesystem; production builds explicitly select and embed `frontend/dist`.
- Dependencies: approved specification only.
- Owned files: `embed.go`, `embed_production.go`, `embed_test.go`, `testdata/static/frontend/dist/index.html`.
- Proving tests: root tests without `frontend/dist`; production-tag build fails without `frontend/dist`; production-tag build succeeds after the frontend build and serves real bundle paths.
- Branch/worktree: current clean-checkout worktree only.
- Rollback: remove the tagged embed split and fixture together.

### Slice 2: Make packaging entries explicit

- Outcome: Make, Docker, GoReleaser, and source documentation consistently select `-tags=production` for distributable binaries while `make test` remains network-free.
- Dependencies: slice 1.
- Owned files: `Makefile`, `Dockerfile`, `.goreleaser.yaml`, `.goreleaser-dev.yaml`, `README.md`.
- Proving tests: static inspection, `make test`, representative local production frontend-plus-Go build.
- Branch/worktree: current clean-checkout worktree only.
- Rollback: revert entry-point flags and documentation with slice 1.

## Parallelization map

Sequential: slice 2 depends on the build constraint and fixture contract established by slice 1. No delegation or parallel writes.

## Integration order

1. Add a failing root regression test for the test filesystem contract.
2. Implement the tagged embedding split and committed fixture.
3. Update every production entry and documentation.
4. Run clean-checkout, broader, negative-production, and positive-production verification.

## Risks and rollback

- Risk: a packaging path omits the production tag. Mitigation: enumerate all current entries and test their configuration.
- Risk: placeholder content enters a distributable. Mitigation: production code cannot reference the fixture and fails compilation when `frontend/dist` is absent.
- Risk: generated output dirties the checkout. Mitigation: remove ignored build output after positive verification and check status.
- Rollback is atomic across both slices; retaining only one would violate the approved specification.
