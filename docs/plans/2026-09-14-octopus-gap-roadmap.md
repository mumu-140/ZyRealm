# Octopus Gap Adoption Roadmap

> Status: staged adoption index. P0.1, P0.1H, and P0.2 are merged and verified on `main`. P0.3 is now the next eligible slice, but remains source-audit-only until a fresh plan is written against the then-current code.
>
> Current verified runtime baseline: ZyRealm `main@fe61b4dd7649a5b45de2fec19e2806ad74704830` (2026-09-15).

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

### P0.2 — Global model filter — MERGED + VERIFIED

Upstream reference: Octopus commit `d5a893ff124ca1cb56f2eb25e14380347d247ae9`.

The useful upstream semantic contract was retained: a system-level `model_filter_regex` composes with channel-level `MatchRegex` as an intersection. ZyRealm could not copy the upstream handler patch verbatim because discovery also flows through batch refresh, scheduled sync, managed-site grouped discovery, session/site fallbacks, and direct-token paths.

Fresh source audit corrected two assumptions from the initial docs-only plan:

1. existing ZyRealm channel filtering and the referenced Octopus implementation use `regexp2.ECMAScript`, not `regexp2.RE2`; P0.2 therefore preserves the existing ECMAScript dialect;
2. managed-channel ownership is path-specific: scheduled sync continues to rely on the existing `ChannelUpdate` managed read-only guard, batch refresh retains its existing `BypassManagedCheck` behavior, and site sync continues to own site projection. P0.2 did not broaden or redesign those ownership semantics.

The implementation adds a shared dependency-light `internal/utils/modelmatch` matcher, backend validation for `model_filter_regex`, and enforcement across audited discovery/sync paths:

- manual model fetch;
- batch model refresh;
- scheduled model sync;
- managed-site grouped primary/fallback discovery;
- direct-token/site snapshot admission.

Policy semantics:

- empty global regex = disabled;
- global + channel regex = intersection;
- provider model order remains stable;
- saving the setting does not immediately rewrite existing channel models or launch a global resync;
- valid regex with zero matches is an authoritative policy result when the upstream discovery was authoritative;
- invalid runtime regex is an error/non-authoritative result and must not erase historical models or fail open.

The Sync Tasks UI exposes the setting with English, Simplified Chinese, and Traditional Chinese copy. Backend validation remains authoritative; no browser-side regex engine became policy authority.

Detailed corrected source audit, implementation record, TDD evidence, and completion record: [`2026-09-14-global-model-filter-plan.md`](./2026-09-14-global-model-filter-plan.md).

Merge/verification evidence:

- initial docs-only source-audit PR: `#21 docs(plan): source-audit P0.2 global model filter` (superseded by the corrected completion record);
- implementation PR: `#22 feat(model): add global model discovery filter`;
- final PR head: `834fec88cf2a24f28b29b0c708913df71d843071`;
- final PR-triggered CI: `#540`, run `34924619996` — governance, backend Vet/full tests, frontend lint/test/build all success;
- squash merge commit: `fe61b4dd7649a5b45de2fec19e2806ad74704830`;
- merged-tree CI: `#541`, run `34926801554` — governance, backend Vet/full tests, frontend lint/test/build all success;
- no DB migration, dependency change, relay routing/failover change, automatic bulk resync, P0.3 code, or P1 schema work.

**Execution order:** P0.2 is closed. P0.3 may now begin only with a fresh source audit against the current `main`; do not implement P0.3 from the remembered scope below.

### P0.3 — Live request state + manual interrupt — QUEUED, SOURCE AUDIT NEXT

Remembered scope only: expose current in-flight routing state and allow a scoped manual interruption of the active attempt/round. ZyRealm should surface its own richer runtime concepts (provider/key/protocol, `RoutingDecision`, `DispatchState`, replay-safety, failure scope) rather than copy Octopus's state object verbatim.

Exact persistence model, SSE/WS transport, retention, interrupt semantics, authorization, UI placement, and interaction with replay safety are intentionally undecided until a fresh P0.3 source audit is complete.

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
- [x] P0.2 source audit + detailed plan completed, with pre-implementation ECMAScript correction recorded.
- [x] P0.2 implemented with RED/GREEN coverage across normal and managed-site discovery paths.
- [x] P0.2 final PR head passed governance, backend Vet/full tests, and frontend lint/test/build.
- [x] P0.2 merged/stabilized on `main`; merged-tree CI `34926801554` all green.
- [ ] P0.3 source audit + detailed plan.
- [ ] P0.3 implemented and merged with full regression evidence.
- [ ] Re-evaluate whether P1 is still necessary after P0 operational feedback.
