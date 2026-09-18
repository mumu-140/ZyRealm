# ZyRealm Plans Index

This directory contains both active execution records and historical design snapshots.

## Execution authority

For any new task, resolve truth in this order:

1. current `main` source and Git history;
2. `AGENTS.md`, `docs/octopus-development-governance.md`, and `docs/octopus-production.md`;
3. current GitHub CI for the exact commit being reviewed;
4. the current task's audit/plan;
5. older plan documents in this directory.

Older plans preserve design provenance. Baseline-specific words such as **current**, **next**, or **remaining** inside an older plan must not be treated as live execution status after later slices have merged.

## Current verified adaptive-routing baseline

At the start of the post-P5D2 normalization audit:

- `main`: `a487ffbadc2fbf8652f66e83891e7788173e15c3`;
- P5D2: merged via PR #58;
- merged-tree CI: run `35365675975`, governance/backend/frontend all successful;
- open PRs: none.

The completed routing sequence is:

`P4C circuit retirement -> P5A coordinator contract -> P5B WS effects -> P5C sidepath coordinator/trace -> P5D1 retry-directive authority -> P5D2 credential fairness`.

## Current routing invariants

- The legacy configurable Circuit Breaker is retired from active relay routing and settings.
- Runtime eligibility is scoped to provider, provider × model, and credential state, with passive half-open recovery.
- A wire result is classified once into `RoutingDecision`; `AttemptCoordinator` projects control flow and side effects without reclassifying raw status/error text.
- Core, Images, Responses Compact, and WebSocket live paths use provider-local fair credential selection.
- WebSocket warmup uses a non-charging fair preview.
- Core HTTP has request-local provider/wire attempt budgets plus a bounded unknown-outcome replay allowance.
- Provider and wire budget settings default to 20 and accept any positive administrator-defined integer.

## Deliberately deferred semantic differences

These are not cleanup defects and must not be changed under a normalization-only task:

1. **Attempt-budget convergence**
   - Core HTTP uses the shared request-level provider/wire budget.
   - Images keeps its existing total `MaxRetries + 1` upstream-start ceiling.
   - Compact and WebSocket keep their own same-channel retry ceilings.
2. **Capacity/RPM admission convergence**
   - Core HTTP and Images enforce channel concurrency/RPM admission.
   - Compact and WebSocket do not currently add those gates.
3. **Compatibility naming**
   - Go module/import paths, `OCTOPUS_*` environment variables, and established runtime/binary/container names may remain `octopus` where changing them would break compatibility.
   - Public product/repository/release identity is ZyRealm.

Any future convergence of (1) or (2) requires a fresh source audit, RED/GREEN behavioral tests, and a separate PR.

## Plan status map

| Document | Status | Use |
| --- | --- | --- |
| `2026-09-19-post-p5d2-repository-normalization-audit.md` | current task record | repository/document/governance normalization after P5D2 |
| `2026-09-17-p4c-circuit-retirement-plan.md` | completed historical slice | breaker retirement provenance |
| `2026-09-18-p5a-attempt-coordinator-contract-plan.md` | completed historical slice | coordinator contract provenance |
| `2026-09-18-p5b-ws-coordinator-effects-plan.md` | completed historical slice | WS effect migration provenance |
| `2026-09-18-p5c-sidepath-coordinator-trace-plan.md` | completed historical slice | sidepath coordinator/trace provenance |
| `2026-09-18-p5d1-retry-directive-authority-plan.md` | completed historical slice | directive authority provenance |
| `2026-09-18-p5d2-credential-fairness-convergence-plan.md` | completed historical slice | fair credential convergence provenance |
| `adaptive-relay-routing-80-percent-plan.md` | historical design snapshot | original routing strategy and acceptance rationale |
| `adaptive-relay-routing-final-review.md` | historical design snapshot | pre-implementation design review against GPT-Load/failure corpus |
| `2026-09-14-octopus-gap-roadmap.md` | historical roadmap snapshot | earlier adoption sequence; not current NEXT authority |
