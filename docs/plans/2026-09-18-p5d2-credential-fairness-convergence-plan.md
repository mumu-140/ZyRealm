# P5D2 Credential Fairness Convergence Plan

> Baseline: `main@48a610acdaac370114fe4454140f77a1a6cc08dc`
>
> Scope: make live Images, Responses Compact, and WebSocket requests use the existing provider-local equal-weight credential scheduler without changing provider ordering or other routing policy.

## 1. Source-audit conclusion

Core HTTP already uses `selectFairChannelCredential`, which combines:
- credential revision availability;
- request-local exclusions;
- sticky preferred key handling;
- provider-local equal-weight fairness;
- cooldown rejection trace events.

Images, Compact, and WS live requests still use `Channel.GetChannelKey`, whose fallback policy is lowest `TotalCost`. Therefore the same credential pool receives different traffic distribution depending on the relay entrypoint.

This conflicts with the existing repository invariant that equal-weight credential fairness remains authoritative.

## 2. Live-request authority

Images, Compact, and WS live relay attempts must use `selectFairChannelCredential`.

The scheduler remains provider-local:
- provider ordering and weights remain owned by `balancer.Iterator`;
- credential selection happens only after a provider candidate is chosen;
- sticky preferred keys still win when eligible;
- preferred selections are charged to the same fair ledger;
- request-local excluded keys remain excluded;
- credential cooldown/revision remains the sole credential admission authority.

P5D2 does not change the existing retry-directive behavior from P5D1.

## 3. WS warmup exception

WS warmup is observational/best-effort and must NOT consume a fair allocation.

However, warmup sets sticky state so that the first live request reuses the warmed credential. Continuing to choose warmup credentials by `TotalCost` would therefore bias the live fair scheduler through sticky preference.

P5D2 adds a **non-charging fair preview**:

```text
WS warmup
   -> filter enabled + credential-available keys
   -> PeekCredentialFair (no progress/sequence mutation)
   -> warm connection
   -> set sticky

first live request
   -> selectFairChannelCredential(preferred sticky)
   -> charge exactly one real allocation
```

The preview must follow the same ordering/revision/re-entry/consecutive-guard semantics as the live scheduler while leaving the ledger accounting unchanged.

## 4. Required distribution behavior

With three enabled credentials whose `TotalCost` values differ drastically, six independent non-sticky live requests through each sidepath must allocate 2/2/2.

This is intentionally independent of historical spend.

Sticky requests may remain sticky; the preferred credential is still charged to the fair ledger.

## 5. Explicit non-goals

P5D2 MUST NOT:
- change provider/channel ordering or weights;
- change P5D1 RetryDirective authority;
- change retry counts/defaults;
- add/remove capacity or RPM gates;
- add provider/wire/replay attempt budgets;
- expand route learning;
- alter credential cooldown durations or revision semantics;
- change WS replay/reconnect/reset behavior;
- change DB schema;
- deploy or touch production.

## 6. RED / GREEN gates

RED must prove:
- Images/Compact/WS live requests still concentrate on lowest-`TotalCost` keys;
- live sidepath source still uses `Channel.GetChannelKey`;
- WS warmup lacks a non-charging fair preview.

GREEN must prove:
- six independent live requests distribute 2/2/2 through Images, Compact, and WS;
- live sidepaths use `selectFairChannelCredential`;
- WS warmup uses `peekFairChannelCredential`, not the charging selector;
- `PeekCredentialFair` does not consume progress/sequence/consecutive accounting;
- P5D1 directive contracts remain green;
- exact-head GitHub CI is fully green.
