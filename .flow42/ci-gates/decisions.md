# Decisions: Add pull-request CI for build and test gates

## 2026-08-27: classify as medium risk

- Context: the requested change controls repository-wide pull-request evidence and executes networked dependency installation on external CI infrastructure.
- Options: low, medium, high, or critical.
- Decision: medium.
- Rationale: the workflow is reversible and has no product runtime, deployment, migration, money, sensitive-data, or repository-write requirement, but CI supply-chain and false-green risks require explicit design and review.
- Consequences: specification must define immutable Action pins, least privilege, and fail-closed handling of the known absent fixtures.
- Actor: codex.
- Approved artifact hash: none; intent approval is pending.

## 2026-08-27: stop at intent gate

- Context: the dispatch authorizes durable intent-stage artifacts only.
- Decision: create the complete work-item artifact set, record read-only evidence, transition to `intent-gate`, and perform no later lifecycle or Forge action.
- Rationale: intent approval is a mandatory human gate.
- Consequences: specification and implementation remain pending exact-digest approval.
- Actor: codex.
- Approved artifact hash: none; intent approval is pending.

## 2026-08-27: accept authenticated intent approval

- Context: comment `5445577497` was supplied as approval provenance for the intent artifact.
- Options: remain at the gate; trust the supplied identifier; or authenticate the author, association, unchanged body, canonical URL, and exact local digest before advancing.
- Decision: authenticated `gh api` read-back verified owner `stefanriegel` approved the unchanged `.flow42/ci-gates/intent.md` SHA-256 `6ec36e77dd5131850cbcd5b14e11ab6fc367fa3b2050bb27ab2a8e27f91ccdec` for specification only; advance through `drafting-spec` to `spec-gate`.
- Rationale: the Forge identity, `OWNER` association, artifact path, exact digest, timestamps, and canonical URL satisfy the approval-provenance contract.
- Consequences: the intent is immutable while approval remains in force; implementation and all later lifecycle actions remain unauthorized.
- Actor: codex.
- Approved artifact hash: `6ec36e77dd5131850cbcd5b14e11ab6fc367fa3b2050bb27ab2a8e27f91ccdec`.

## 2026-08-27: use structural fail-closed known-fixture verification

- Context: the approved repository-wide Go test command currently fails because fixture files are absent, but CI must not hide unrelated regressions.
- Options: broadly allow failure; skip packages/tests; add fixtures outside scope; or parse the complete `go test -json` stream against a closed manifest and reject every deviation.
- Decision: require the full package run and a repository-owned structural verifier that accepts only the exact known failing packages, tests, and missing paths, with negative self-tests for false-green cases.
- Rationale: the complete run stays visible while new failures, changed failure causes, malformed evidence, and unexpected recovery all fail the gate.
- Consequences: the manifest is explicit temporary debt and must be removed or revised through review when fixtures are restored; the affected tests cannot provide semantic coverage beyond their missing-file failure until then.
- Actor: codex.
- Approved artifact hash: pending specification approval.

## 2026-08-27: accept authenticated specification approval

- Context: comment `5445617752` was supplied as approval provenance for the specification artifact.
- Decision: authenticated `gh api` read-back verified owner `stefanriegel` approved the unchanged `.flow42/ci-gates/spec.md` SHA-256 `3b8b7350e4673cfbd8856a82d754fc994b7c1b0df54da0ffc7a64026d97f2f82` for planning and implementation.
- Rationale: author, owner association, immutable body, artifact path, digest, timestamp, and canonical URL match the local artifact and requested gate.
- Consequences: planning and implementation are authorized; commit, push, pull-request writes, merge, release, and deployment remain unauthorized.
- Actor: codex.
- Approved artifact hash: `3b8b7350e4673cfbd8856a82d754fc994b7c1b0df54da0ffc7a64026d97f2f82`.
