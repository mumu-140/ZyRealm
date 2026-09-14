# Octopus Gap Adoption Roadmap

> Status: planning index only. This file exists to prevent useful upstream ideas from being forgotten; it is intentionally **not** a full implementation plan for every item.
>
> Baseline: ZyRealm `main` at `cbe4638b01aa5beb1a46f73dfb41cabaecaf890c` (2026-09-14).

## Planning rule

Adopt upstream ideas one slice at a time.

1. Deep-read the ZyRealm source paths touched by the current slice.
2. Write a detailed implementation plan for that slice only.
3. Implement it, run the full relevant regression suite, and merge it cleanly.
4. Only then deep-read and plan the next slice.
5. Do **not** lock P1 schema or architecture while P0 is still moving.

This is deliberate: Octopus and ZyRealm now have materially different runtime architectures. We should transfer useful ideas, not copy upstream patches mechanically.

## P0 sequence

### P0.1 — Client-header templates — ACTIVE

Allow channel custom-header **values** to explicitly reference safe metadata from the original client request, using syntax such as:

```text
X-Upstream-Project: {client_header:OpenAI-Project}
X-Tenant-Context: tenant-{client_header:X-Tenant-ID}
```

Upstream reference: Octopus commit `1c48ee5105042b8eebaba05c05b2773b04e6c7f3` (`client_header`).

ZyRealm-specific requirement: preserve the existing credential/header-isolation boundary and make HTTP ingress and WebSocket ingress behave consistently. Client authorization/API-key/cookie/proxy-auth material must never become an arbitrary template source.

Detailed source-audited plan: [`2026-09-14-client-header-template-plan.md`](./2026-09-14-client-header-template-plan.md).

**Gate before P0.2 planning:** P0.1 implementation merged with HTTP + WS + persistence-validation + security regression coverage and full CI green.

### P0.2 — Global model filter — QUEUED, NOT YET SOURCE-AUDITED

Remembered scope only: add a system-level model-discovery filter that composes with channel-level filtering (intended semantics: both must pass). Exact configuration ownership, regex engine, managed-channel behavior, cache invalidation, API shape, and tests are intentionally undecided until P0.1 is complete.

Upstream reference: Octopus commit `d5a893ff124ca1cb56f2eb25e14380347d247ae9`.

**Do not implement or write a detailed plan from this paragraph.** Re-read the then-current ZyRealm model discovery/sync code first.

### P0.3 — Live request state + manual interrupt — QUEUED, NOT YET SOURCE-AUDITED

Remembered scope only: expose current in-flight routing state and allow a scoped manual interruption of the active attempt/round. ZyRealm should surface its own richer runtime concepts (provider/key/protocol, `RoutingDecision`, `DispatchState`, replay-safety, failure scope) rather than copy Octopus's state object verbatim.

Exact persistence model, SSE/WS transport, retention, interrupt semantics, authorization, UI placement, and interaction with replay safety are intentionally undecided until P0.2 is complete.

**Do not implement or write a detailed plan from this paragraph.** Re-read the then-current relay/log/runtime-state code first.

## P1 — DEFERRED UNTIL ALL P0 GATES PASS

### Credential × model × protocol capability model

Remembered idea only: Octopus's `ChannelKey` / `ChannelModel` / `ChannelGrant` split is a useful authorization-modeling reference. ZyRealm already has a more advanced runtime credential pool, so any future P1 work must layer capability metadata onto the existing scheduler rather than replace it.

No schema, migration, API, or runtime design is approved here. P1 must be source-audited and planned only after P0 is complete and stable.

## Explicit non-goals

- No wholesale merge/rebase from Octopus for these features.
- No P1 database migration while P0 is in progress.
- No replacement of ZyRealm credential fairness, cooldown, circuit breaking, failure-domain classification, protocol fallback, or replay-safety logic with Octopus's simpler routing model.
- No speculative planning of later slices based on today's file layout; each slice must be planned against the then-current `main`.

## Progress checklist

- [x] Gap inventory captured.
- [x] P0.1 source audit completed enough to write an implementation plan.
- [x] P0.1 detailed plan recorded.
- [ ] P0.1 implemented and merged with full regression evidence.
- [ ] P0.2 source audit + detailed plan.
- [ ] P0.2 implemented and merged with full regression evidence.
- [ ] P0.3 source audit + detailed plan.
- [ ] P0.3 implemented and merged with full regression evidence.
- [ ] Re-evaluate whether P1 is still necessary after P0 operational feedback.
