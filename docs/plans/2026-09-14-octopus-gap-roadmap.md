# Octopus Gap Adoption Roadmap

> Status: staged adoption index. P0.1 and P0.1H are merged and verified on `main`. P0.2 has now been source-audited against the current code and has a detailed implementation plan, but no P0.2 production code has been written.
>
> P0.2 planning baseline: ZyRealm `main@786c1c6c94a2228d47b971e2720b86523a091ba5` (2026-09-14).

## Planning rule

Adopt upstream ideas one slice at a time.

1. Deep-read the ZyRealm source paths touched by the current slice.
2. Write a detailed implementation or diagnostic plan for that slice only.
3. Implement it, run the full relevant regression suite, and merge it cleanly.
4. Only then deep-read and plan the next slice.
5. Do **not** lock P1 schema or architecture while P0 is still moving.

This is deliberate: Octopus and ZyRealm now have materially different runtime architectures. We should transfer useful ideas, not copy upstream patches mechanically.

## P0 sequence

### P0.1 — Client-header templates — MERGED + VERIFIED

Allow channel custom-header **values** to explicitly reference safe metadata from the original client request, using syntax such as:

```text
X-Upstream-Project: {client_header:OpenAI-Project}
X-Tenant-Context: tenant-{client_header:X-Tenant-ID}
```

Upstream reference: Octopus commit `1c48ee5105042b8eebaba05c05b2773b04e6c7f3` (`client_header`).

ZyRealm-specific implementation keeps template metadata structurally separate from request bodies and ordinary WebSocket header forwarding. It uses a sanitized request-scoped template source, rejects credential/cookie/proxy/forwarded/WebSocket-control sources, validates configuration before persistence, renders per request/attempt without mutating cached channels, and includes final rendered headers in WebSocket pool identity.

Detailed source-audited plan: [`2026-09-14-client-header-template-plan.md`](./2026-09-14-client-header-template-plan.md).

Merge/verification evidence:

- PR: `#16 feat(relay): support safe client header templates`;
- squash merge commit: `c3e1e5a018b136ee61c7e2696275ea314f916dcd`;
- merged-tree GitHub Actions CI run: `34831468603`;
- governance: success;
- backend: `go vet ./...` success and `go test -buildvcs=false ./...` success;
- frontend: lint, tests, and production build success.

### P0.1H — First-token timeout after heartbeat must fail over — MERGED + VERIFIED

Production evidence was collected read-only and sanitized before the fix. The preserved report is [`../debug/2026-09-14-relay-failover-diagnostic.md`](../debug/2026-09-14-relay-failover-diagnostic.md), with structured companion [`../debug/2026-09-14-relay-failover-diagnostic.jsonl`](../debug/2026-09-14-relay-failover-diagnostic.jsonl).

The diagnostic covered 12 requests / 18 real attempts and established three important boundaries:

- three first-token-timeout incidents were stopped because infrastructure heartbeat/header output had been mistaken for real downstream model delivery;
- three final `context canceled` incidents had authoritative outer-context cancellation and were correctly terminal;
- two `INTERNAL_ERROR; received from peer` stream failures occurred after real model protocol payload had already been emitted and were correctly terminal.

The first-token timeout classifier itself was already correct. The defect was the commitment signal: for HTTP streaming, an infrastructure SSE heartbeat could make the generic Gin response writer look written before any provider payload had been delivered. The routing layer then treated the timeout as post-commit and terminal based on the wrong signal.

The hotfix keeps the existing unified routing policy and changes only what counts as delivery commitment:

- WebSocket: commitment requires an actual downstream event;
- HTTP streaming: commitment requires actual provider stream payload;
- non-stream HTTP: ordinary response-writer commitment remains authoritative.

A deterministic real-handler regression exercises the failure mode end-to-end: provider A waits through an early heartbeat and reaches first-token timeout, then provider B succeeds and its SSE payload reaches the client. No timeout duration, replay allowance, retry budget, failure scope, circuit behavior, or cooldown policy was broadened.

The same slice adds optional bounded `failover_stop_reason` observability to the existing serialized attempt routing trace:

- `downstream_committed`
- `client_canceled`
- `no_alternative`
- `unknown_replay_budget`
- `wire_attempt_budget`
- `provider_attempt_budget`
- `candidate_exhausted`

Later gates late-bind the reason to the originating attempt span, and the first concrete reason wins so a specific reason is not overwritten by generic exhaustion.

Detailed source audit, runtime evidence, TDD evidence, reference-repository comparison, and completion record: [`2026-09-14-relay-failover-error-audit.md`](./2026-09-14-relay-failover-error-audit.md).

Reference-repository result: current upstream Octopus has a substantially simpler relay loop and no equivalent ZyRealm `AttemptRoutingTrace` / replay-budget / failure-domain machinery. Its explicit request-state idea was useful, but its handler was not copied verbatim; the implementation reuses ZyRealm's existing `RoutingDecision` and `AttemptSpan` architecture.

Merge/verification evidence:

- sanitized diagnostic PR: `#18 chore(debug): add sanitized relay failover evidence`, diagnostic head `adba0bea2c2c4252e9126080e653405493af7d0f`;
- implementation PR: `#19 fix(relay): fail over after heartbeat-only first-token timeout`;
- final PR head: `76346f581421b0f9393e59b77c20c40ae8e1e676`;
- final PR-triggered CI run: `34849018591` — governance, backend Vet/full tests, and frontend lint/test/build all success;
- squash merge commit: `12b3a6952739fac85f678002f0d0e8c77190e446`;
- merged-tree CI run: `34860648315` — governance, backend Vet/full tests, and frontend lint/test/build all success;
- no database migration, dependency change, frontend feature, deployment script, timeout-duration change, broad retry rule, P0.2/P0.3/P1 implementation.

The audited `INTERNAL_ERROR` samples were post-payload failures, so P0.1H deliberately did **not** add a broad peer-error substring classifier. Any future pre-output peer-stream case remains evidence-driven.

**Execution order:** P0.1H is closed. P0.2 may now proceed from its source-audited plan.

### P0.2 — Global model filter — SOURCE-AUDITED + PLAN READY, NOT IMPLEMENTED

Fresh audit baseline: `main@786c1c6c94a2228d47b971e2720b86523a091ba5`.

Detailed plan: [`2026-09-14-global-model-filter-plan.md`](./2026-09-14-global-model-filter-plan.md).

Upstream reference: Octopus commit `d5a893ff124ca1cb56f2eb25e14380347d247ae9`.

The upstream semantic contract is retained: a global `model_filter_regex` and the existing channel `MatchRegex` compose as an intersection, using the same backend `regexp2.RE2` behavior. ZyRealm must adapt the application points rather than copy the upstream handler patch verbatim.

The current source audit found four important ZyRealm-specific requirements:

- the setting can use the existing key/value settings store and cache, so no database migration is needed;
- global admission must cover manual fetch, batch refresh, scheduled sync, and managed-site primary/fallback discovery;
- regular sync must continue to skip site-managed projection channels because site sync owns them;
- saving the setting must not trigger an immediate bulk rewrite: it applies on the next relevant fetch/refresh/sync, using existing diff/report/persistence paths.

A valid filter that matches zero models is policy. An invalid regex is an error and must not be converted into an authoritative empty list or silently fail open.

The plan introduces a small dependency-light shared matcher so setting validation, existing channel filtering, normal discovery, and managed-site fallback discovery use one backend regex contract without making `helper` depend on global `op` state.

**Execution order:** implement P0.2 with TDD from the detailed plan, verify the final merged `main` tree, then begin the P0.3 source audit. Do not start P0.3 while P0.2 is only planned.

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
- No blanket `if error then switch provider` handling that ignores downstream commitment or client cancellation provenance.
- No speculative planning of later slices based on today's file layout; each slice must be planned against the then-current `main`.

## Progress checklist

- [x] Gap inventory captured.
- [x] P0.1 source audit completed enough to write an implementation plan.
- [x] P0.1 detailed plan recorded.
- [x] P0.1 implementation completed with full regression evidence.
- [x] P0.1 merged/stabilized on `main`.
- [x] First-token-timeout failover requirement captured.
- [x] P0.1H source audit + detailed hotfix plan recorded.
- [x] Sanitized production routing evidence collected and preserved.
- [x] P0.1H defect confirmed by production trace and deterministic real-handler heartbeat + timeout reproduction.
- [x] P0.1H delivery-commitment fix implemented with TDD.
- [x] P0.1H bounded failover stop-reason observability implemented and regression-tested.
- [x] P0.1H final PR head passed governance, backend Vet/full tests, and frontend lint/test/build.
- [x] P0.1H final diff/review boundary audited.
- [x] P0.1H merged/stabilized on `main`; merged-tree CI `34860648315` all green.
- [x] P0.2 source audit + detailed plan.
- [ ] P0.2 implemented and merged with full regression evidence.
- [ ] P0.3 source audit + detailed plan.
- [ ] P0.3 implemented and merged with full regression evidence.
- [ ] Re-evaluate whether P1 is still necessary after P0 operational feedback.
