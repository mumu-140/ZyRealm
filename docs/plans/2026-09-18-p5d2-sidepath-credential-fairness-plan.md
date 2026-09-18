# P5D2 Sidepath Credential Fairness Plan

> Baseline: `main@48a610acdaac370114fe4454140f77a1a6cc08dc`
>
> Scope: make live Images, Responses Compact, and WebSocket requests use the existing provider-local equal-weight credential fair ledger without changing sidepath retry, capacity, route-learning, or warmup semantics.

## 1. Source-audit conclusion

Core already selects a credential with `selectFairChannelCredential` before running same-channel transport retries. The fair allocation is charged when a credential is selected; `runProtocolRetries` may retry the same credential without selecting or charging it again.

Images / Compact / WS live paths still use `Channel.GetChannelKey`, which prefers the enabled credential with the lowest historical `TotalCost`. That makes credential distribution depend on accumulated cost instead of the repository's existing equal-weight provider-local scheduler.

## 2. Authority rule

For live relay requests:

```text
provider candidate
    -> availability-filtered credential candidates
    -> provider-local fair ledger
    -> selected credential
    -> transport retries on that credential without re-charging
    -> ROTATE_CREDENTIAL selects and charges another credential
```

Sticky preference remains authoritative when present; `SelectCredentialFair` already charges sticky selections to the same ledger.

## 3. Sidepath-specific migration

### Images

Images currently re-runs key selection for every upstream start. P5D2 must separate credential selection from same-credential transport retry so `RETRY_SAME_CREDENTIAL` does not create an extra fair allocation.

### Compact / WebSocket

P5D1 already keeps `usedKey` across `RETRY_SAME_CREDENTIAL` and invokes a selection closure only for initial selection or credential rotation. Replace only that live selection closure with `selectFairChannelCredential`.

## 4. Warmup boundary

`bestEffortWarmupUpstreamWS` is observational/background preconnect work. It MUST NOT call `selectFairChannelCredential` or `availability.SelectCredentialFair`, because doing so would consume a live-routing allocation before any user request.

Warmup keeps non-accounting `Channel.GetChannelKey` selection plus availability filtering.

## 5. Preserved boundaries

P5D2 MUST NOT:
- change `RetryDirective` semantics;
- change retry counts/defaults;
- add Compact/WS capacity or RPM admission;
- change Images capacity/RPM admission;
- add provider/wire/replay budgets to sidepaths;
- expand route learning;
- change credential cooldown/revision semantics;
- change WS replay/reset/reconnect behavior;
- change DB schema;
- deploy.

## 6. RED / GREEN evidence

RED must prove that extreme `TotalCost` skew still pins Images, Compact, and WS live requests to the cheapest key.

GREEN must prove:
- six live requests across three eligible keys distribute 2/2/2 on each sidepath despite extreme `TotalCost` skew;
- live sidepaths call `selectFairChannelCredential`;
- same-credential retry does not reselect/recharge the fair ledger;
- WS warmup still does not consume fair allocation;
- existing credential cooldown, sticky, P5D1 directive, P5C trace/effect, and full CI contracts remain green.
