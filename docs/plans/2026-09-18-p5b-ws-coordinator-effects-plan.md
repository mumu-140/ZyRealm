# P5B WS Coordinator Effects Plan

> Baseline: `main@ce15c53ef85593b6f86f352e676632b37de43500`
>
> Scope: make Core HTTP and WebSocket consume the shared coordinator effect projection without changing WebSocket retry/session semantics.

## 1. Source-audit conclusion

P5A established a pure `RoutingDecision -> attemptCoordination` projection and connected Core HTTP post-decision handling.

WebSocket already shares `relayAttempt` and therefore already receives a canonical, traced `RoutingDecision`, but its post-decision effect application still reads the decision directly:

- credential failure: `decision.Domain == failureDomainCredential`
- runtime availability: `recordRuntimeAvailabilityEvidence(... result ...)`
- outlier scope: `decision.OutlierScope`

At the same time, WS retry/session behavior is intentionally transport-specific and must remain unchanged:

- same-channel retry still uses the existing `isRetryableStatus(result.StatusCode)` gate;
- exact replay keeps the 15-second child timeout;
- exact replay keeps the maximum of 3 candidate channels;
- continuation reconnect/redial stays in the WS transport layer;
- conversation-reset semantics stay in the WS session layer;
- route-learning coverage is NOT expanded to WS.

## 2. P5B seam

The safe seam is after the existing WS same-channel retry loop:

```text
WS transport attempt / retry loop
            |
            v
canonical attemptResult + RoutingDecision
            |
            v
AttemptCoordinator
            |
            +-- runtime effect
            +-- credential-failure effect
            +-- outlier scope
            |
            v
existing WS success/reset/canceled/written/session handling
```

The coordinator does not move ahead of the retry loop in P5B.

## 3. Runtime-effect application

P5B adds a no-classification runtime-effect applier that accepts the coordinator's `attemptEffectPlan`.

Core HTTP and WS use that explicit effect plan.

The existing `recordRuntimeAvailabilityEvidence` compatibility helper remains for Images / Compact and tests that still supply raw `attemptResult`; those paths are not migrated in P5B.

## 4. Core effect completeness

Core already consumes:
- outlier scope,
- replay safety,
- provider skip,
- content-policy signal.

P5B additionally makes Core consume:
- runtime effect from the coordinator effect plan;
- credential-failure projection where the credential loop rotates keys;
- route-learning candidacy from the coordinator effect plan.

This is a pure projection migration. It does not change the underlying `RoutingDecision`.

## 5. Explicit non-goals

P5B MUST NOT:
- change WS retry count or `Group.MaxRetries` interpretation;
- remove or replace `isRetryableStatus` in the WS retry loop;
- add provider/wire/replay budgets to WS;
- add capacity/RPM admission to WS;
- migrate WS to the fair credential scheduler;
- expand managed-route learning to WS;
- change exact-replay 15-second timeout;
- change exact-replay 3-candidate cap;
- change continuation affinity, reconnect, redial, or conversation reset;
- touch Images / Compact orchestration;
- add DB migration or deployment changes.

## 6. RED / GREEN gates

RED must prove the current code has not yet migrated:
- WS lacks `coordinateAttemptOutcome`;
- WS still reads credential/outlier effects directly;
- Core runtime application still bypasses the effect projection.

GREEN must prove:
- Core and WS runtime availability consume `attemptEffectPlan`;
- WS credential/outlier effects consume coordinator fields;
- Core route-learning/credential effects consume coordinator fields;
- coordinator source remains free of classification logic;
- WS retry/session invariants remain present;
- Images / Compact do not call `coordinateAttemptOutcome`;
- exact-head GitHub CI is green.
