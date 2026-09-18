# P5C Images + Compact Coordinator / Trace Parity Plan

> Baseline: `main@7412022e419140d305b0cebdeb56a972ed6d9813`
>
> Scope: migrate Images and Responses Compact post-decision effects to AttemptCoordinator and bring their persisted attempt traces to the same RoutingDecision vocabulary as Core/WS, without converging transport or retry policy.

## 1. Fresh source-audit findings

### Images

Images already has the pieces required for a safe migration:

- every real upstream start owns a `balancer.AttemptSpan`;
- `imagesAttemptRoutingResult` adapts transport facts into `attemptResult`;
- `withRoutingDecision` already computes the canonical policy verdict for every attempt;
- runtime availability, credential cooldown, and outlier evidence are already applied.

The remaining P5 gap is consumption: Images still reads `RoutingDecision` effects directly and still uses the compatibility runtime helper. Its `AttemptSpan` is carried by `attemptResult`, but no routing trace is attached.

Images retry/admission semantics are explicitly out of scope:
- `maxUpstreamStarts = Group.MaxRetries + 1` remains unchanged;
- channel concurrency and RPM gates remain unchanged;
- `Channel.GetChannelKey` ordering remains unchanged;
- same-request retry remains `credential failure OR isRetryableStatus(status)`.

### Responses Compact

Compact also already computes a canonical `RoutingDecision`, but only after the transport retry loop.

Its observability gap is structural: `forwardResponsesCompact` creates and ends the `AttemptSpan` internally, then discards the span before `compactAttemptRoutingResult` classifies the outcome.

P5C therefore returns the existing span as transport bookkeeping so classification can attach trace after `End`. `AttemptSpan.SetRoutingTrace` and `SetRoutingRuntime` already support late updates, so no attempt-counting or transport-order change is required.

For trace parity, each Compact transport attempt is adapted/classified immediately after that attempt returns. The existing retry gate remains status-based; only the final attempt's effect plan is applied, preserving pre-P5C runtime/credential/outlier mutation semantics.

Compact policy boundaries remain unchanged:
- same-channel retry remains `isRetryableStatus(statusCode)`;
- `Group.MaxRetries` interpretation remains unchanged;
- no concurrency/RPM admission is added;
- no fair credential scheduler is introduced;
- no managed-route learning is added.

## 2. Shared sidepath trace adapter

P5C adds a small helper that attaches the already-computed decision to an existing `AttemptSpan`:

```text
attemptResult + valid RoutingDecision + credential revision
        |
        v
routingAttemptTrace(...)
        |
        v
AttemptSpan.SetRoutingTrace(...)
```

It performs no classification and owns no retry/failover policy.

Provider/wire attempt indices remain zero for Images/Compact because those sidepaths do not own the Core request attempt budget in P5C.

## 3. Effect migration

After a valid `attemptCoordination` is projected:

### Images consumes
- runtime effect;
- credential-failure flag;
- outlier scope.

### Compact consumes
- runtime effect;
- credential-failure flag;
- outlier scope.

Neither path expands managed-route learning in P5C.

## 4. Trace parity target

Persisted real attempts from Images and Compact must expose the same inspector vocabulary already used by Core/WS:

- failure domain;
- failure scope;
- rule ID;
- retry directive;
- runtime effect/state;
- outlier effect;
- replay safety;
- dispatch state;
- downstream commitment;
- credential revision.

This is validated through RelayLog subscription, not only source inspection.

## 5. Explicit non-goals

P5C MUST NOT:
- change Images retry count/defaults or `MaxRetries + 1` semantics;
- remove Images concurrency/RPM admission;
- introduce capacity/RPM admission to Compact;
- migrate either sidepath to fair credential scheduling;
- add provider/wire/replay budgets;
- expand managed-route learning to Images or Compact;
- change response/error passthrough behavior;
- change multipart/SSE/compact body transport;
- modify DB schema or deployment.

## 6. RED / GREEN gates

RED must demonstrate:
- Images and Compact do not yet consume `coordinateAttemptOutcome`;
- both still consume direct decision effects / compatibility runtime helper;
- persisted sidepath RelayLog attempts are missing routing-decision trace fields.

GREEN must demonstrate:
- both paths consume coordinator runtime/credential/outlier effects;
- both paths persist routing trace for real attempts;
- Compact can attach trace/runtime after its transport span has ended;
- Images retry/admission invariants remain present;
- Compact retry semantics remain present and capacity/RPM/fair-scheduler additions remain absent;
- AttemptCoordinator remains classification-free;
- exact-head GitHub CI is fully green.
