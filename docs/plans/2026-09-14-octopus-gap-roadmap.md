# Octopus Gap Adoption Roadmap

> Status: staged adoption index. P0.1 is merged and verified on `main`; P0.1H is implemented and fully CI-verified on its feature branch, with final integration still pending. Later P0/P1 work remains intentionally queued.
>
> Current verified baseline: ZyRealm `main@c3e1e5a018b136ee61c7e2696275ea314f916dcd` (2026-09-14).

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

### P0.1H — First-token timeout after heartbeat must fail over — IMPLEMENTED + VERIFIED, AWAITING INTEGRATION

Observed terminal symptom:

```text
channel failed: failed to send request: first token timeout (30s)

failed to send request: first token timeout (30s)
```

Source audit showed that ZyRealm already classified first-token timeout correctly as a next-provider event when downstream output was not committed. The defect was the commitment signal: for HTTP streaming, an infrastructure SSE heartbeat could make the generic Gin response writer look written before any provider payload had been delivered. The routing layer then correctly treated the timeout as post-commit and terminal, but based on the wrong signal.

The hotfix keeps the existing unified routing policy and changes only what counts as delivery commitment:

- WebSocket: commitment requires an actual downstream event;
- HTTP streaming: commitment requires an actual provider stream payload;
- non-stream HTTP: ordinary response-writer commitment remains authoritative.

A deterministic real-handler regression now exercises the exact failure mode: provider A waits through an early heartbeat and reaches first-token timeout, then provider B succeeds and its SSE payload reaches the client. No timeout duration, replay allowance, retry budget, failure scope, circuit behavior, or cooldown policy was broadened.

The same slice also makes valid terminal outcomes explainable through an optional bounded `failover_stop_reason` in the existing serialized attempt routing trace:

- `downstream_committed`
- `client_canceled`
- `no_alternative`
- `unknown_replay_budget`
- `wire_attempt_budget`
- `provider_attempt_budget`
- `candidate_exhausted`

Later gates late-bind the reason to the originating attempt span, and the first concrete reason wins so a specific reason is not overwritten by a generic exhaustion reason.

Detailed source audit, deterministic reproduction, TDD evidence, reference-repository comparison, and merge checklist: [`2026-09-14-relay-failover-error-audit.md`](./2026-09-14-relay-failover-error-audit.md).

Reference-repository result: current upstream Octopus has a substantially simpler relay loop and no equivalent ZyRealm `AttemptRoutingTrace` / replay-budget / failure-domain machinery. Its explicit request-state idea is useful, but its handler is not safe to copy verbatim; the implementation therefore reuses ZyRealm's existing `RoutingDecision` and `AttemptSpan` architecture.

Verified code evidence before the final documentation fold:

- branch: `codex/first-token-timeout-heartbeat-failover`;
- code head: `5692cc99be10334b14b7e11e02b2abc43f4c96de`;
- GitHub Actions CI run: `34845934726`;
- governance: success;
- backend Vet + full Go tests: success;
- frontend lint/test/build: success;
- no database migration, dependency, frontend feature, deployment script, P0.2/P0.3/P1 implementation.

A production incident trace was not available through the current repository tooling and has not been invented. The exact defect was instead proven with deterministic real-handler integration reproduction. The documentation-inclusive final branch head must still pass fresh CI and PR boundary review before merge.

Other audited-looking error families such as pre-output HTTP/2 peer `INTERNAL_ERROR` remain evidence-driven follow-ups and are not silently folded into this first-token-timeout hotfix.

**Execution order:** merge/stabilize P0.1H before beginning the P0.2 source audit.

### P0.2 — Global model filter — QUEUED, NOT YET SOURCE-AUDITED

Remembered scope only: add a system-level model-discovery filter that composes with channel-level filtering (intended semantics: both must pass). Exact configuration ownership, regex engine, managed-channel behavior, cache invalidation, API shape, and tests are intentionally undecided until P0.1H is complete.

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
- [ ] Sanitized production incident trace collected. Not available in current tooling; no values fabricated.
- [x] P0.1H defect confirmed by deterministic real-handler heartbeat + timeout reproduction.
- [x] P0.1H delivery-commitment fix implemented with TDD.
- [x] P0.1H bounded failover stop-reason observability implemented and regression-tested.
- [x] P0.1H code head passed governance, backend Vet/full tests, and frontend lint/test/build.
- [ ] P0.1H final documentation-inclusive head passes fresh CI and PR boundary review.
- [ ] P0.1H merged/stabilized on `main`.
- [ ] P0.2 source audit + detailed plan.
- [ ] P0.2 implemented and merged with full regression evidence.
- [ ] P0.3 source audit + detailed plan.
- [ ] P0.3 implemented and merged with full regression evidence.
- [ ] Re-evaluate whether P1 is still necessary after P0 operational feedback.
