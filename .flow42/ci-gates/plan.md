# Plan: Add pull-request CI for build and test gates

## Vertical slices

### Slice 1: Closed known-fixture verifier

- Outcome: the complete repository-wide Go test stream is parsed structurally and accepted only for the specification's exact missing-fixture debt.
- Owned files: `cmd/verify-known-fixtures/main.go`, `cmd/verify-known-fixtures/main_test.go`.
- Proving tests: verifier unit tests for the accepted baseline and every required false-green rejection; actual `go test -json ./... -count=1` evidence.
- Rollback: remove the verifier and its workflow invocation together.

### Slice 2: Least-privileged pull-request workflow

- Outcome: one stable check performs clean-checkout tests, the explicit known-fixture gate, locked frontend build, and production embedding proof.
- Dependencies: slice 1.
- Owned files: `.github/workflows/ci.yml`, `scripts/verify-production-embed.sh`, workflow static tests.
- Proving tests: static workflow contract tests, clean-checkout root test, frontend build, production-tag test, and loopback proof.
- Rollback: remove the workflow and production proof script together.

## Parallelization map

Slices are sequential because the workflow consumes the verifier. No delegation or parallel writes.

## Integration order

1. Implement and self-test the closed verifier.
2. Add the production loopback proof with bounded readiness and cleanup.
3. Add the pinned, least-privileged workflow in the specification's required order.
4. Run static, clean-checkout, actual-known-failure, frontend, and production checks locally.

## Risks and rollback

- False green from a broadened exception: closed manifest plus negative unit tests.
- Supply-chain drift: exact tool inputs, frozen lockfile, immutable Action commits.
- Leaked process or generated residue: trap-based cleanup and final worktree inspection.
- Rollback is the removal of the new CI-only files; no product or fixture content changes.
