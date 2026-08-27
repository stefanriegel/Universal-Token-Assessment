# Decisions: Make the test entry deterministic from a clean checkout

For each decision record context, options, decision, rationale, consequences,
actor, timestamp, and the approved artifact hash when applicable.

## D-001: Accept the authenticated intent approval

- Context: The work item was paused at `intent-gate` pending durable approval.
- Options: remain at the gate; accept an unauthenticated assertion; verify the Forge approval and advance.
- Decision: Authenticated `gh` read-back verified owner `stefanriegel` approved the exact `intent.md` SHA-256 `55f75d46ff98c2a320bd4e81702677da81375cb1e4e3c1b04a4935a646fc7329` in comment `5445297262`; advance to specification.
- Rationale: The author, artifact, digest, canonical URL, and unchanged update time satisfy the Flow42 approval-provenance contract.
- Consequences: The intent is immutable while its approval remains in force; any intent edit invalidates downstream approvals.
- Actor: `codex`
- Timestamp: `2026-08-27T21:17:10Z`

## D-002: Separate clean-checkout test assets from fail-closed production embedding

- Context: Raw Go test compilation must work without ignored frontend output, while distributable binaries must contain the real generated UI.
- Options: generate `frontend/dist` before tests; commit generated output; allow a shared fallback; define distinct test and production embedding paths.
- Decision: Require a small repository-owned test filesystem for clean-checkout compilation and an explicitly selected, fail-closed production embedding path for every build/release entry.
- Rationale: This keeps tests deterministic and network-free without allowing placeholder content to satisfy a production build.
- Consequences: Planning must enumerate every production entry and proving tests must cover both the clean-checkout path and negative/positive production asset provenance.
- Actor: `codex`
- Timestamp: `2026-08-27T21:18:40Z`
- Specification SHA-256: `916755d8aa6c7403dbd57c7f50652cacd9cb2c17dc783fa71d58c78f1844990b`

## D-003: Accept the authenticated specification approval and execute a low-risk plan

- Context: The work item was paused at `spec-gate`; comment `5445355881` was supplied as approval provenance.
- Options: remain at the gate; trust the supplied identifier without verification; authenticate and bind the approval to the local bytes.
- Decision: Authenticated `gh` read-back verified owner `stefanriegel` approved the unchanged specification digest; persist that provenance, complete the narrow sequential plan, and advance to building.
- Rationale: The approval author, exact digest, immutable timestamps, and canonical URL match the local artifact and low-risk work does not require a separate plan gate.
- Consequences: The specification is immutable and implementation is authorized. Commit, branch push, and pull-request creation are reversible; merge, release, deployment, publication, and other irreversible actions remain unauthorized without the applicable gate.
- Actor: `codex`
- Timestamp: `2026-08-27T21:23:30Z`
- Specification SHA-256: `916755d8aa6c7403dbd57c7f50652cacd9cb2c17dc783fa71d58c78f1844990b`

## D-004: Select explicit production build tags and stop at PR readiness

- Context: Go does not automatically define a build constraint during `go test`, so production and test assets require an explicit packaging distinction.
- Options: generate ignored assets for tests; share a fallback; select test assets by default and require an explicit production build tag on every packaging entry.
- Decision: Use a repository-owned default test filesystem and a `production`-tagged real embed file; update Make, Docker, GoReleaser, and documentation, then verify and stop at `pr-ready`.
- Rationale: Test commands stay deterministic and network-free while every distributable path fails compilation if the real bundle is missing.
- Consequences: Ad hoc production builds must include `-tags=production`; the documented and automated entries encode it. Only merge, release, deployment, publication, and other irreversible actions require the irreversible-action gate.
- Actor: `codex`
- Timestamp: `2026-08-27T21:28:30Z`
