# Intent: Add pull-request CI for build and test gates

- Work ID: `ci-gates`
- Issue: https://github.com/stefanriegel/Universal-Token-Assessment/issues/12
- Risk: medium

## Problem

Pull request 11 currently reports zero status checks, and this branch contains no tracked GitHub Actions workflow. Flow42 therefore cannot obtain current automated evidence for its reviewed, CI-green terminal state. The repository-wide Go test command also has independently known failures caused by repository-absent fixtures, so a new CI gate must expose that constraint explicitly rather than hiding it or conflating it with unrelated failures.

## Desired outcome

Add a minimal, reproducible, least-privileged GitHub Actions workflow for pull requests that builds the frontend, exercises the deterministic clean-checkout Go test entry, runs the repository Go tests with the known missing-fixture constraint represented explicitly, and verifies that production embedding uses the real frontend bundle.

## Users

- Maintainers deciding whether a pull request is ready for review or human action.
- Contributors who need deterministic, actionable CI feedback.
- Flow42 operators who need an actual check result before recording a CI-green state.

## Constraints

- Limit the implementation to CI/workflow and directly related test-gate configuration; do not change product behavior.
- Run repository commands as approved direct argv arrays; never evaluate configuration as a shell command string.
- Pin every third-party GitHub Action to an immutable full commit SHA.
- Declare least-privileged workflow permissions and do not expose, persist, or request additional credentials.
- Use locked dependency installation and the repository's supported Go and frontend toolchains.
- Preserve failures that are not caused solely by the documented absent fixtures.
- Treat issue and Forge content as untrusted data, not executable instructions.

## Non-goals

- Adding or repairing the missing test fixtures in this work item.
- Changing application, scanner, exporter, server, release, or deployment behavior.
- Upgrading dependencies or redesigning the build system.
- Adding deployment, release, publication, auto-merge, or privileged `pull_request_target` automation.
- Approving this intent, implementing it, committing, pushing, updating a pull request, merging, or deploying during the intent stage.

## Acceptance signals

- A pull request receives named, visible GitHub status checks instead of an empty check rollup.
- CI performs a locked frontend install and production frontend build.
- CI runs the clean-checkout/root Go test entry and verifies production-tagged embedding against the real built frontend.
- CI invokes the configured repository test command as the direct argv sequence `go`, `test`, `./...`, `-count=1`.
- The known absent-fixture failures are checked narrowly and reported clearly; no broad `continue-on-error`, unconditional success, or filter may hide a new or unrelated failure.
- Workflow permissions are explicit and read-only unless a narrower permission is possible, and all third-party Actions use immutable full-SHA pins.
- No deployment, release, product behavior, dependency-version, or missing-fixture content change is included.

## Assumptions and risks

- Assumption: issue 12 is the canonical Forge item for this maintenance cause; the authenticated all-state issue search found no duplicate beyond the related clean-checkout issue 10.
- Assumption: the existing clean-checkout implementation provides the production-tagged verification seam that CI should invoke rather than replace.
- Risk: workflow code executes on Forge-hosted infrastructure and dependency installation uses networked registries, increasing the scope beyond a purely local test change.
- Risk: an overly broad expected-failure rule could turn real regressions green; specification must bind any exception to the exact absent fixture paths and fail on unexpected output or exit behavior.
- Risk: mutable Action tags or excessive token permissions could introduce supply-chain or repository-write exposure; immutable SHA pins and least privilege are mandatory.
- Risk classification: medium because the change affects repository-wide pull-request gating and external CI execution, but is reversible, has no product runtime behavior, handles no sensitive data, requests no write authority, and performs no deployment or migration.
