# Evidence: Add pull-request CI for build and test gates

## Preflight and configuration

- Timestamp: 2026-08-27T21:42:04Z.
- Repository: `stefanriegel/Universal-Token-Assessment`; branch `flow42/clean-checkout-test-entry`; HEAD `50f91d3`.
- `gh auth status` verified the active authenticated GitHub account `stefanriegel` with repository read access.
- Fresh SHA-256 of `.flow42/config.yml`: `676313c6e72ede3dbae856d01b6f1ab223e8a3779d0d2a5bd1e087ae12ccded5`.
- `.flow42/config-approval.yml` records that same digest and the canonical owner comment https://github.com/stefanriegel/Universal-Token-Assessment/issues/10#issuecomment-5445319247.
- Authenticated `gh api` read-back verified comment `5445319247` is unchanged, authored by `stefanriegel`, has `OWNER` association, and explicitly approves the matching configuration digest as the execution policy.
- The approved test command is a direct argv array: `[go, test, ./..., -count=1]`; no scalar command was accepted or evaluated.

## Maintenance-signal and deduplication evidence

- Authenticated `gh issue view 12` read issue 12 as open, authored by `stefanriegel`, with the requested pull-request CI scope and explicit no-deploy, no-release, no-dependency-upgrade, and no-product-behavior boundaries.
- Authenticated all-state issue listing found issue 12 as the canonical CI-gates item. Issue 10 is related clean-checkout and missing-fixture context, not a duplicate CI-workflow request.
- No Forge content was executed or written.

## Current zero-check evidence

- Authenticated structured query of pull request 11, whose head is `flow42/clean-checkout-test-entry`, returned `statusCheckRollup: []`: exactly zero current status checks.
- `git ls-files '.github/workflows/*'` and `git ls-tree -r --name-only HEAD '.github/workflows'` both returned no paths: the current branch has zero tracked workflow files.
- Historical workflow runs from July 2026 exist on other commits, but they do not supply a current check for pull request 11 and are not evidence of a usable present gate.

## Known missing-fixture constraint

- Timestamp: 2026-08-27T21:41:30Z to 2026-08-27T21:41:56Z.
- Read-only baseline command, using the approved direct argv contract with network-disabled Go module resolution: `GOPROXY=off GOSUMDB=off go test ./... -count=1`.
- Expected: reach the repository package tests and preserve any known absent-fixture failures for explicit CI design.
- Actual: exit code 1. Root and most packages passed; failures were attributable to absent repository files rather than the earlier root embed setup problem.
- `internal/exporter` and `server` require absent `testdata/nios-metric-fields.json`; `internal/exporter` also requires absent `testdata/cross-source-fixture.json`.
- `internal/scanner/nios` requires absent `testdata/ms-allocation/unavailable.xml`, the scenario XML files, and corresponding JSON goldens under `testdata/ms-allocation/`.
- This intent does not authorize adding those fixtures. Any later CI exception must be exact, observable, and fail closed for unexpected failures.

## Known gaps

- No specification approval, implementation, workflow, commit, push, pull-request update, or external write has been performed.

## Intent approval read-back

- At 2026-08-27T21:45:54Z, authenticated `gh api repos/stefanriegel/Universal-Token-Assessment/issues/comments/5445577497` returned author `stefanriegel`, association `OWNER`, identical creation/update time `2026-08-27T21:43:45Z`, and canonical URL https://github.com/stefanriegel/Universal-Token-Assessment/issues/12#issuecomment-5445577497.
- Exact approval body: `Flow42 intent approval:\n\n- artifact: \`.flow42/ci-gates/intent.md\`\n- SHA-256: \`6ec36e77dd5131850cbcd5b14e11ab6fc367fa3b2050bb27ab2a8e27f91ccdec\`\n\nApproved for specification; this does not authorize merge, deployment, publication, or another irreversible action.`
- Fresh `shasum -a 256 .flow42/ci-gates/intent.md` matched `6ec36e77dd5131850cbcd5b14e11ab6fc367fa3b2050bb27ab2a8e27f91ccdec` before the canonical transition from `intent-gate` to `drafting-spec`.

## Specification evidence

- Specification SHA-256: `3b8b7350e4673cfbd8856a82d754fc994b7c1b0df54da0ffc7a64026d97f2f82`.
- Authenticated GitHub API tag/commit resolution selected immutable pins: `actions/checkout@11d5960a326750d5838078e36cf38b85af677262` (v4), `pnpm/action-setup@b906affcce14559ad1aafd4ab0e942779e9f58b1` (annotated v4 tag dereferenced to its commit), `actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020` (v4), and `actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16` (v6).
- Toolchain pins derive from repository evidence: Go `1.25.6` in `go.mod`, Node `22.12.0` satisfying README Node 22+ and current Vite engine constraints, and locally verified pnpm `10.30.1`.
- A fresh `go test -json ./... -count=1` baseline exited 1 and identified only the three known failing packages and enumerated fixture-dependent tests captured in the specification. The specification requires complete structural parsing and negative self-tests; it forbids `continue-on-error`, broad filtering, and package/test omission.
- No workflow or product file was created or changed. Work stopped at `spec-gate`.

## Specification approval read-back

- At 2026-08-27T21:50:00Z, authenticated `gh api repos/stefanriegel/Universal-Token-Assessment/issues/comments/5445617752` returned author `stefanriegel`, association `OWNER`, identical creation/update time `2026-08-27T21:48:17Z`, and canonical URL https://github.com/stefanriegel/Universal-Token-Assessment/issues/12#issuecomment-5445617752.
- The unchanged comment binds `.flow42/ci-gates/spec.md` to SHA-256 `3b8b7350e4673cfbd8856a82d754fc994b7c1b0df54da0ffc7a64026d97f2f82` and authorizes planning and implementation only.
- Fresh local hashing matched that digest before transition through planning to building.

## Implementation and local verification

- Added one `pull_request` workflow with workflow-level `contents: read`, credential persistence disabled, concurrency cancellation, one Ubuntu job, exact Go `1.25.6`, Node `22.12.0`, pnpm `10.30.1`, and the four specification-approved immutable Action commit SHAs.
- The clean-checkout/root gate passed with `frontend/dist` absent before and after: `go test . -count=1` exited 0. Static workflow contract coverage runs in that root package and confirms the trigger, permissions, tool/action pins, exact commands, four-action limit, and forbidden constructs.
- The repository-wide gate ran the complete `go test -json ./... -count=1` package set and exited 1. The repository-owned structural verifier accepted exactly 16 failing test events in `internal/exporter`, `internal/scanner/nios`, and `server`, with 14 observed allowlisted missing paths; it rejected synthetic exit-zero, malformed/truncated JSON, missing failure, unexpected test, unexpected path, and non-fixture diagnostic cases.
- Before frontend generation, `go build -tags=production` exited 1 with `embed_production.go:7:12: pattern all:frontend/dist: no matching files found`, preserving fail-closed behavior.
- `pnpm --dir frontend install --frozen-lockfile` completed with pnpm `10.30.1` entirely from cached packages and did not change the lockfile. `pnpm --dir frontend build` succeeded with Vite `8.0.11` and produced the real hashed bundle.
- `go test -tags=production . -run '^TestProductionEmbedUsesRealFrontendBundle$' -count=1` passed; a production-tagged binary then served a non-empty real `index.html` and `assets/index-BQWxPXkj.js` on a loopback ephemeral port, while the proof rejected the test-fixture marker and cleaned up the process.
- Generated `frontend/dist` and `frontend/node_modules` were moved out of the checkout after verification. `git diff --check` passed; no product source, fixture, dependency lock, credential, generated bundle, or binary was added.
- Work stopped at `verifying`. No commit, push, pull-request or issue update, merge, deployment, release, publication, or other Forge write was performed.
