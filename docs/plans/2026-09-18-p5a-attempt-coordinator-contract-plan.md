# P5A AttemptCoordinator Contract Plan

> Baseline: `main@b73e62f43d0d75607c84ea4749600fd3603db47f`
>
> Scope: establish the smallest shared attempt-orchestration contract without changing routing semantics.

## 1. Source-audit findings

The relay paths already share classification and health primitives, but they do not yet share one orchestration authority.

### Core HTTP

Core HTTP already owns the complete chain:

`Iterator -> candidate admission -> fair credential selection -> protocol plans -> relayAttempt -> RoutingDecision -> runtime/outlier/route-learning effects -> replay guard -> provider failover -> trace`

It is the reference behavior for P5, not a reason to rewrite the other transports in one step.

### Images

Images already uses:
- `availability.AcquireCandidate`
- concurrency/RPM admission
- credential revision availability
- `RoutingDecision`
- runtime availability effects
- credential cooldown
- outlier evidence

But it still owns a separate retry loop, uses `Channel.GetChannelKey`, has no provider/wire attempt budget, and decides retry from status/domain rather than consuming the full routing directive.

### Responses Compact

Compact already uses:
- runtime availability
- credential revision availability
- `RoutingDecision`
- runtime availability effects
- credential cooldown
- outlier evidence

It intentionally keeps its own buffered `/responses/compact` transport and body-model rewrite. It currently has no concurrency/RPM gate, no shared attempt budget, and no full routing-trace attachment.

### WebSocket

WS already reuses `relayAttempt` and `RoutingDecision`, but its outer orchestration is transport/session specific:
- round-scoped live request control
- exact-replay 15-second child budget
- exact-replay candidate cap
- continuation affinity
- reconnect-before-replay
- conversation reset semantics

Those remain transport/session responsibilities.

## 2. P5A invariant

P5A introduces a single narrow seam:

```text
transport attempt
      |
      v
attemptResult
      |
      | classification happens before this boundary
      v
RoutingDecision (precomputed)
      |
      v
AttemptCoordinator
      |
      +-- disposition plan
      +-- effect plan
```

The coordinator MUST consume an already-valid `RoutingDecision`.

It MUST NOT:
- parse upstream error text;
- inspect status codes to derive retry/failover policy;
- call `withRoutingDecision`, `classifyRoutingFailure`, `fallbackStatus`, or `isRetryableStatus`;
- choose providers, credentials, protocol plans, or transports;
- own provider/wire/replay budgets;
- change capacity/RPM admission;
- change route-learning coverage;
- change retry counts or defaults.

## 3. Contract

The first contract is deliberately pure.

`coordinateAttemptOutcome(result attemptResult)` returns false when no valid decision is attached. It never synthesizes one.

For a valid decision it projects two explicit outputs:

### Disposition

- routing directive
- provider skip
- same-credential retry
- terminal state
- replay safety

### Effects

- runtime effect
- outlier scope
- route-learning candidacy
- credential-failure flag
- content-policy flag

The projection must preserve the decision exactly, even if an intentionally inconsistent raw `attemptResult` would have classified differently.

## 4. P5A implementation boundary

P5A may:
1. add the pure coordinator contract;
2. make Core HTTP consume that contract after `withRoutingDecision`;
3. add regression/source tests proving the coordinator never reclassifies.

P5A does NOT migrate Images, Compact, or WS control flow yet.

That migration belongs to later slices after the contract is green and reviewed.

## 5. Later slices

- **P5B**: Core + WS consume shared outcome/effect contract while preserving WS session/reconnect semantics.
- **P5C**: Images + Compact adapt transport outcomes to the contract; add trace parity.
- **P5D**: separately review semantic convergence for fair credentials, attempt budgets, capacity/RPM, route learning, and retry policy.

Any such convergence is a policy change and must not be hidden inside a refactor.

## 6. Delivery gates

P5A is complete only when:
- coordinator source has no routing reclassification dependency;
- Core behavior remains unchanged under existing tests;
- no Images/Compact/WS retry-policy change is present;
- no DB migration or deployment change exists;
- exact-head GitHub CI is fully green.
