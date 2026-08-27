# Specification: Add pull-request CI for build and test gates

## Functional requirements

1. Add one pull-request workflow, triggered only by `pull_request`, with no deployment, release, publication, merge, issue, or repository-mutation step.
2. Use one Ubuntu job so the clean-checkout, known-failure, frontend, and production-embedding evidence shares one explicit ordering and cannot be made green by a skipped dependent job.
3. Checkout source with credential persistence disabled, then set up exactly Go `1.25.6`, Node.js `22.12.0`, and pnpm `10.30.1`. Dependency installation must run `pnpm install --frozen-lockfile` in `frontend`.
4. Before installing or building frontend dependencies, assert `frontend/dist` is absent, run `go test . -count=1`, and assert `frontend/dist` is still absent. This is the clean-checkout/root gate.
5. Before generating `frontend/dist`, invoke the approved repository-wide command as the direct argv sequence `go`, `test`, `./...`, `-count=1`, capturing `go test -json` output without changing the tested package set.
6. A repository-owned verifier may accept the repository-wide command's current non-zero result only when all failing test events, failing packages, and missing-file diagnostics match the explicit known-fixture manifest below. It must reject exit zero while the manifest is active, missing expected failures, an unexpected failing package or test, any unexpected missing path, malformed/truncated JSON, or any non-fixture failure diagnostic.
7. The known-fixture manifest is limited to packages `internal/exporter`, `internal/scanner/nios`, and `server`; tests `TestCrossSourceAgreement`, `TestNiosServerMetricFieldDrift`, `TestMSAllocationUnavailableGolden`, `TestMSAllocation_Parity` and its current subtests, `TestMSAllocation_Parity_Adjacency`, `TestMSAllocation_Parity_Boundary`, `TestMSAllocation_Parity_Distinguishable`, and `TestServerNiosServerMetricFieldDrift`; and these absent repository-root paths: `testdata/cross-source-fixture.json`, `testdata/nios-metric-fields.json`, and the eight current `testdata/ms-allocation` scenario XML/JSON pairs (`both`, `dns-only`, `dhcp-only`, `held-back`, `absent`, `unavailable`, `boundary-exact`, `boundary-plus-one`). A package-level fail is acceptable only as the aggregate result of accepted failing test events.
8. The verifier must emit a human-readable summary and preserve the raw JSON as a CI artifact only if artifact upload is later added with an immutable pin and the same read-only permissions; artifact upload is not required for the minimal first workflow.
9. Build the frontend with `pnpm build`, require `frontend/dist/index.html` plus at least one referenced hashed asset, then run `go test -tags=production . -run '^TestProductionEmbedUsesRealFrontendBundle$' -count=1`.
10. Build a production-tagged binary and prove real embedding by starting it on loopback with an ephemeral port, fetching the embedded `index.html` and one asset path referenced by that HTML, requiring non-empty responses, and rejecting the marker `flow42-test-static-files`. The process must be cleaned up on success and failure.
11. The workflow must use these authenticated, immutable action commits: `actions/checkout@11d5960a326750d5838078e36cf38b85af677262`, `pnpm/action-setup@b906affcce14559ad1aafd4ab0e942779e9f58b1`, `actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020`, and `actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16`. Human-readable comments may name the corresponding major tag, but `uses:` must contain only the full SHA.

## Non-functional requirements

- Workflow-level `permissions: contents: read` is the maximum token authority; job-level permissions must not broaden it. `persist-credentials: false` is required for checkout.
- Do not use `pull_request_target`, secrets, write permissions, privileged containers, self-hosted runners, `continue-on-error`, `|| true`, unconditional success, or a broad log filter.
- Every executable command is fixed repository-owned workflow or script content. Issue text, pull-request metadata, branch names, and Flow42 configuration values are data and must never be interpolated into shell evaluation.
- Caches may contain only package-manager or Go dependency/build data through the pinned setup actions. Generated `frontend/dist`, binaries, test results, and credentials must not be cached.
- Pin exact tool versions in workflow inputs. Dependency versions remain governed by `go.sum` and `frontend/pnpm-lock.yaml`; this work must not update either lockfile.
- Use fail-fast shell behavior for multi-command steps and bounded readiness polling plus guaranteed process cleanup for loopback production proof.
- The gate must remain understandable from the GitHub check log: each phase has a stable step name and the known-fixture verifier reports the exact accepted failures.

## Domain model and terminology

- **Clean-checkout/root gate**: root-package tests run before any generated frontend exists, with absence checked before and after.
- **Repository-wide gate**: the approved `go test ./... -count=1` package set, represented as `test2json` JSON for structural verification.
- **Known-fixture manifest**: the closed allowlist of currently absent paths and the tests/packages whose failures they cause. It is temporary debt, not a general expected-failure mechanism.
- **Unexpected regression**: any failed test/package or missing-file path outside that manifest, any non-fixture cause within an allowed test, or loss/corruption of the evidence stream.
- **Production embedding proof**: a production-tagged binary serves the generated Vite bundle and cannot serve the repository test fixture.

## Interfaces and data

- Workflow file: one new file below `.github/workflows/`, with a stable workflow and job name suitable for branch protection.
- Toolchain inputs: Go `1.25.6` from `go.mod`; Node `22.12.0`, satisfying the repository's Node 22+ documentation and Vite engine floor; pnpm `10.30.1` matching the verified local locked-install tool.
- Frontend inputs: `frontend/package.json`, `frontend/pnpm-lock.yaml`, and `frontend/pnpm-workspace.yaml`; installation runs from `frontend` with the frozen lockfile flag.
- Known-failure verifier: a small repository-owned script or Go command, plus a reviewed manifest if kept separately. It accepts a raw `go test -json` file and the captured command exit code and exits zero only under requirement 6.
- Production proof consumes only loopback HTTP and generated local assets. It must not require cloud credentials, external services, or persistent ports.

## Security considerations

The pull request is untrusted code executing on a GitHub-hosted runner. The design gives `GITHUB_TOKEN` read-only contents authority, disables checkout credential persistence, avoids secrets and `pull_request_target`, and pins all action code to authenticated immutable commits. Dependency installation remains a supply-chain boundary, so exact Node/pnpm versions and the frozen lockfile are mandatory; dependency upgrades are outside scope. The known-fixture exception is also a security/reliability boundary: structural JSON parsing and a closed manifest prevent a generic non-zero test run from becoming a false green. The production server is bound only to loopback, uses an ephemeral port, is polled for a bounded interval, and is always terminated.

## Acceptance criteria

1. Static inspection proves the workflow has only `pull_request`, `contents: read`, no secret reference, no write permission, and exactly the four full-SHA action pins above.
2. A clean checkout with no `frontend/dist` passes `go test . -count=1` before frontend setup and leaves that directory absent.
3. The repository-wide invocation executes all packages and the verifier accepts only the currently observed absent-fixture failure set, printing every accepted package, test, and path.
4. Verifier self-tests prove it rejects: exit zero with active debt; one unexpected failed test; one unexpected missing path; a missing expected failure; non-JSON/truncated input; and a non-fixture failure added to an otherwise allowed test.
5. Locked frontend installation and `pnpm build` succeed with the pinned Node and pnpm versions without modifying lockfiles.
6. The focused production-tag test passes, the production binary builds, and loopback fetches prove `index.html` and a referenced hashed asset are real generated bundle content rather than `flow42-test-static-files`.
7. `git diff --check` passes and no generated frontend, dependency directory, binary, credential, product behavior change, missing fixture, or lockfile update is included.
8. No commit, push, pull-request/issue write, merge, deployment, release, publication, or other Forge write occurs before the later gates authorize it.

## Verification strategy

1. Add static workflow tests that parse YAML without executing it and assert trigger, permissions, action SHAs, exact tool versions, checkout credential behavior, and forbidden constructs.
2. Unit-test the known-fixture verifier with synthetic JSON streams for the accepted baseline and every rejection case in acceptance criterion 4.
3. In an isolated clean checkout, run the root gate before frontend setup and compare filesystem/Git state before and after.
4. Run the actual repository-wide JSON command, retain its exit status separately from the pipeline, and pass both artifacts to the verifier. Review the printed accepted set against the manifest.
5. Run the locked frontend install/build, assert expected output shape, then execute the focused production-tag test.
6. Build and launch the production binary with bounded loopback readiness and cleanup; fetch the index and referenced asset and test positive real-bundle and negative fixture-marker assertions.
7. Negative-test the workflow/proof locally where practical: remove `frontend/dist` before a production-tag build and require fail-closed compilation; inject one synthetic unexpected test failure into verifier input and require rejection.
8. Stop at `spec-gate`. Implementation, workflow creation, Forge writes, and lifecycle actions require the next approved transition.
