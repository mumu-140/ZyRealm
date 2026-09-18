# P5D1 RetryDirective Authority Plan

> Baseline: `main@c4f2fac577eec9a5e75d4f82162dfe392f88c803`
>
> Scope: make Images, Responses Compact, and WebSocket execute the canonical `RoutingDecision.RetryDirective` instead of independently deriving retry direction from HTTP status.

## 1. Source-audit conclusion

P5A-P5C converged classification, effects, and trace, but sidepath control flow still diverges.

Core HTTP already treats `RoutingDecision` as the policy verdict:
- provider-transient decisions skip the current provider;
- credential failures rotate credentials;
- terminal decisions stop;
- replay safety is evaluated before cross-provider replay.

Images / Compact / WS still contain status-driven retry branches such as `isRetryableStatus(statusCode)`. This can contradict the canonical decision.

Examples:

- provider-transient 503 -> canonical `NEXT_PROVIDER`, but Compact/WS can retry the same provider;
- generic unknown 500 -> canonical `RETRY_SAME_CREDENTIAL`, but Images currently excludes the current key and rotates;
- credential 401 -> canonical `ROTATE_CREDENTIAL`, but Compact/WS currently leave the provider instead of rotating within the request;
- explicit content-policy error -> canonical `TERMINAL`, but status-based sidepath loops can still continue.

## 2. Authority rule

`RetryDirective` determines **where routing goes next**.

Existing retry/budget configuration determines only **whether another attempt is still allowed**.

```text
COMPLETE / TERMINAL
    -> stop sidepath routing

RETRY_SAME_CREDENTIAL
    -> same provider, same key, subject to existing sidepath retry budget

ROTATE_CREDENTIAL
    -> same provider, next eligible key, subject to existing sidepath retry budget

NEXT_PROVIDER / NEXT_CANDIDATE
    -> leave current provider immediately

PROTOCOL_OR_PROVIDER
    -> sidepaths in P5D1 have no protocol fallback; leave current provider
```

No sidepath may derive retry direction from raw HTTP status after a canonical decision exists.

## 3. Existing ceilings stay authoritative

P5D1 does not change retry counts or defaults.

### Images

Keep:
- `maxUpstreamStarts = group.MaxRetries + 1` when retry is enabled;
- one total upstream-start budget across providers;
- existing channel concurrency/RPM admission.

The directive only changes whether the next start uses the same key, another key, or another provider.

### Compact

Keep:
- existing `maxSameChannelRetries` calculation;
- no capacity/RPM admission;
- direct key selector until P5D2.

### WebSocket

Keep:
- existing `maxSameChannelRetries` calculation;
- exact-replay 15-second child timeout;
- exact-replay 3-candidate cap;
- continuation/reconnect/reset semantics.

## 4. Sidepath execution adapter

Add a pure adapter that maps only the already-projected `attemptDisposition.Directive` into a sidepath execution action.

It MUST NOT inspect:
- status codes;
- error strings;
- failure domains;
- runtime effects;
- retry configuration.

This is execution adaptation, not a second classifier.

## 5. Explicit non-goals

P5D1 MUST NOT:
- migrate Images/Compact/WS to fair credential scheduling;
- change credential fairness policy;
- add Compact/WS capacity or RPM admission;
- add Core provider/wire/replay budgets to sidepaths;
- expand route learning;
- change `RoutingDecision` classification rules;
- change retry defaults;
- change DB schema;
- deploy or touch production.

## 6. RED / GREEN evidence

RED must demonstrate current divergence with real requests:
- Images generic 500 rotates key instead of retrying the same key;
- Images provider-transient failure spends another start in the same provider;
- Compact credential failure does not rotate to the next key in the same request;
- Compact provider-transient failure retries the same provider;
- WS credential failure does not rotate to the next key in the same round;
- WS provider-transient failure retries the same provider.

GREEN must demonstrate:
- all six cases follow the canonical directive;
- sidepath source no longer calls `isRetryableStatus` to choose retry direction;
- the sidepath execution adapter contains no classifier dependency;
- P5C trace/effect contracts remain green;
- exact-head GitHub CI is fully green.
