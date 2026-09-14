# Octopus Gap Adoption Roadmap

> Status: staged adoption index. P0.1 is merged and verified on `main`; P0.1H is now the active diagnostic/hotfix slice. Later P0/P1 work remains intentionally queued.
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

### P0.1H — Pre-output transport failover + cancellation provenance — ACTIVE DIAGNOSTIC/HOTFIX

Production observations include three error families:

```text
failed to send request: first token timeout (20s/30s)
failed to send request: Post "<upstream>/v1/chat/completions": context canceled
stream read error: stream error: stream ID N; INTERNAL_ERROR; received from peer
```

These errors must **not** be handled by a blanket “any failure -> switch” rule.

Current-main source audit shows that first-token timeout and ambiguous outbound cancellation are already represented in the unified routing model:

- first-token timeout -> `RuleID=first_token_timeout`, `RetryDirective=next_provider`, provider-model scope, bounded unknown-outcome replay when `DispatchState=maybe_sent`;
- outbound/child `context canceled` while the outer client request is still active -> `RuleID=ambiguous_transport_cancel`, request-local next-provider failover;
- actual outer-client cancellation/deadline -> terminal by design;
- any failure after downstream payload commitment -> terminal by design, because replaying another generated stream could splice/duplicate output.

Therefore a production row that ends in `channel failed` despite a retryable-looking message may be caused by a **later routing gate**, not by missing string recognition. Examples include:

- no eligible alternative provider/candidate;
- all alternatives skipped by runtime cooldown, circuit, disabled state, capability negative cache, concurrency, or RPM;
- provider/wire attempt budget exhausted;
- the single bounded unknown-outcome cross-provider replay allowance already consumed;
- downstream response already committed;
- outer client/proxy context actually canceled;
- deployed binary predates the current `main` behavior.

The `stream read error ... INTERNAL_ERROR` family is the remaining likely classifier gap. Required semantic split:

- before any real downstream payload is written -> eligible provider/transport failover candidate;
- after payload is written -> terminal, never splice a second provider's stream.

Current provider-transient marker coverage does not explicitly name `stream read error`, HTTP/2 `INTERNAL_ERROR`, or `received from peer`; this must only be changed after a real event proves `DownstreamCommitted=false`.

Detailed source-audited diagnostic/hotfix plan: [`2026-09-14-relay-failover-error-audit.md`](./2026-09-14-relay-failover-error-audit.md).

Runtime evidence gate before production code changes:

- confirm deployed Git SHA;
- collect 2-5 sanitized incidents for each error family;
- prefer persisted `AttemptRoutingTrace` over raw log strings;
- capture `RuleID`, `FailureDomain`, `FailureScope`, `RetryDirective`, `ReplaySafety`, `DispatchState`, `DownstreamCommitted`, `OuterContextState`, `OutboundContextCause`, `ProviderAttempt`, and `WireAttempt`;
- record whether a next candidate existed and why it was skipped/blocked;
- for peer stream errors, record whether any payload/token had been written before the error.

**Hotfix success criterion:** any recoverable failure that occurs before downstream commitment must enter the existing candidate failover path; terminal behavior remains only when routing/safety/exhaustion policy requires it, and the final trace/log must expose why failover did not continue.

**Execution order:** finish this diagnostic gate and P0.1H implementation/verification before beginning P0.2 source audit.

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
- [x] P0.1H source audit completed to the runtime-evidence gate.
- [x] P0.1H diagnostic/hotfix plan recorded.
- [ ] Sanitized production routing evidence collected for timeout/cancellation/peer-stream failures.
- [ ] P0.1H implementation gap confirmed by runtime trace.
- [ ] P0.1H implemented and merged with regression evidence.
- [ ] P0.2 source audit + detailed plan.
- [ ] P0.2 implemented and merged with full regression evidence.
- [ ] P0.3 source audit + detailed plan.
- [ ] P0.3 implemented and merged with full regression evidence.
- [ ] Re-evaluate whether P1 is still necessary after P0 operational feedback.
