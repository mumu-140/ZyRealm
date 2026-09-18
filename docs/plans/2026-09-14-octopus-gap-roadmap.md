# Octopus Gap Adoption Roadmap

> Status: **HISTORICAL SNAPSHOT**. This roadmap preserves the adoption sequence at its original baseline and is no longer the source of live `NEXT` work. Use `docs/plans/README.md` plus the current `main` source for execution status.
>
> Current verified runtime baseline: ZyRealm `main@5b773f13f84734eb222ad99e35b5840c69d2f188`, with merged-tree CI run `35051237424` all green. This roadmap closeout performs no deployment or production-state change.

## Planning rule

Adopt upstream ideas one slice at a time.

1. Deep-read the ZyRealm source paths touched by the current slice.
2. Write a detailed implementation or diagnostic plan for that slice only.
3. Implement it with RED/GREEN evidence, run the full relevant regression suite, and merge it cleanly.
4. Only then deep-read and plan the next slice.
5. Treat the then-current `main`, repository governance script, CI workflow, and pre-push contract as execution authority; do not invent a parallel validation workflow.
6. Do **not** lock schema-heavy P1 architecture while nearer data-plane compatibility/reliability questions remain unresolved.

This is deliberate: Octopus-derived repositories and ZyRealm now have materially different runtime architectures. We should transfer useful ideas, not copy patches mechanically.

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

- initial docs-only source-audit PR: `#21 docs(plan): source-audit P0.2 global model filter` (superseded and closed unmerged);
- final corrected implementation/closure PR: `#23`;
- squash merge commit: `8c629cddb1b41e28322950736b8089d6c19a6dcf`;
- merged-tree CI: `#543`, run `34931539402` — governance, backend Vet/full tests, and frontend lint/test/build all success;
- ECMAScript matcher semantics and managed-channel ownership boundaries are present on the verified `main` baseline;
- no DB migration, relay routing/failover change, automatic bulk resync, P0.3 code, or P1 schema work.

**Execution order:** P0.2 is closed. P0.3 was source-audited and implemented from `main@8c629cddb1b41e28322950736b8089d6c19a6dcf`, then squash-merged and verified on `main@c60ba21cebe5d3b9424d2fffff97964e2b281deb`.

### P0.3 — Live request state + manual interrupt — MERGED + VERIFIED

Fresh source audit showed that `RelayLog` is completion-oriented and gets its durable ID only at log-save time, while HTTP and downstream Responses WebSocket have different context ownership. P0.3 therefore does **not** turn durable logs into a live-control plane. It adds a separate process-local runtime layer:

- one independent live request ID and `relayControl` per logical HTTP request or executable WebSocket `response.create` round;
- safe snapshots for transport/model/channel-key IDs/protocol/attempt counters/dispatch state/downstream commitment/phase, with no raw request or response content and no credential values;
- explicit `errManualInterrupt` cause, classified as request-scoped terminal and health/circuit/outlier neutral;
- replay-safety remains the existing routing model: pre-dispatch is `not_sent`, MAYBE_SENT is `unknown_upstream_outcome`, and committed delivery is unsafe to replay;
- HTTP control is parented to ingress context and shared by attempt clones;
- each WebSocket round has a child control independent of the connection context, so interrupting one turn leaves the downstream WebSocket usable for the next `response.create`;
- exact-replay local timeout remains a scoped child beneath the round control, preserving the existing 15-second recovery budget;
- authenticated management API only: `GET /api/v1/live-request/list` and `POST /api/v1/live-request/:id/interrupt` under `middleware.Auth()`;
- Logs UI gets a separate Active Requests surface backed by `['live-requests']` and 1-second snapshot polling; durable RelayLog SSE/cache remains unchanged.

Detailed source-audited design and implementation plan:

- [`2026-09-15-p0.3-live-request-manual-interrupt-design.md`](./2026-09-15-p0.3-live-request-manual-interrupt-design.md)
- [`2026-09-15-p0.3-live-request-manual-interrupt-plan.md`](./2026-09-15-p0.3-live-request-manual-interrupt-plan.md)
- [`2026-09-15-p0.3-merge-verification.md`](./2026-09-15-p0.3-merge-verification.md) — authoritative post-merge closure record.

Implementation / verification evidence:

- branch: `codex/p0.3-live-request-interrupt`;
- PR: `#24 feat(relay): P0.3 live request state and manual interrupt`;
- reviewed implementation head before evidence-only docs: `b3edbf65456550a816e3e3e7b5d882d869259f1e`;
- final topic-branch head: `6507d536b4e56413c382f6876cc03abc6b4d5c1e`;
- HTTP lifecycle RED: run `34936995852` — missing HTTP control/live snapshot as expected;
- authenticated route RED/GREEN: runs `34945515796` / `34945890168`;
- WebSocket round-scope RED/GREEN: runs `34946086827` / `34946690318`;
- exact-replay child-context RED/GREEN: runs `34946910561` / `34947110080`;
- frontend Active Requests RED/GREEN: runs `34947301822` / `34947564792`;
- final code-review race RED: run `34948033814` — missing manual-cancel setup-error suppression as expected;
- reviewed implementation GREEN: run `34948368279` — governance, backend `go vet ./...` + full Go tests, frontend lint/tests/build all success;
- final PR-head CI: run `34949893366` — governance, backend Vet/full tests, and frontend lint/tests/production build all success;
- PR scope audit: only P0.3 design/plan, relay runtime/tests, authenticated handler/tests, and Logs-page live-request UI/tests; no DB migration, deploy/compose, dependency, or production-state changes;
- PR review-thread audit: no unresolved review threads before merge;
- squash merge commit: `c60ba21cebe5d3b9424d2fffff97964e2b281deb`;
- merged-tree CI: run `34960012680` (`CI #625`) — governance, backend Vet/full tests, and frontend lint/test/build all success;
- runtime limitation: registry/cancellation is intentionally process-local and valid for the current single application container; multi-replica distributed cancellation is deferred;
- deployment status: **not deployed** by this merge workflow.

## Post-P0 routing observability baseline — MERGED + VERIFIED

Historical Routing Inspector PR #27 is part of the runtime baseline and remains a regression dependency for later relay work.

Verified baseline:

- merge/head commit: `1f4b856b350c3941000e1ca9af12c7ed42f67622`;
- merged-tree CI run: `35006072380` — success;
- request-local typed routing-decision ledger is persisted for historical explanation;
- runtime candidate, credential cooldown, and capability rejection decisions can be explained without placing raw request/response bodies or free-form provider detail into the trace;
- the Inspector is read-only and must remain data-plane neutral.

P1P was therefore accepted only after its TTFT/failover/replay changes and Chat passthrough coverage remained compatible with this routing trace and Inspector contract.

## P1P — Passthrough semantic hardening + same-format coverage — MERGED + VERIFIED

Detailed source-audited implementation and closure plan: [`2026-09-16-p1p-passthrough-hardening-coverage-plan.md`](./2026-09-16-p1p-passthrough-hardening-coverage-plan.md).

The source audit against the pre-P1P baseline corrected the earlier feature-gap interpretation:

- ZyRealm already had the optional `model.PassthroughCapable` architecture; passthrough was **not missing**;
- Anthropic Messages -> Anthropic Messages and OpenAI Responses -> OpenAI Responses already used same-format raw HTTP passthrough;
- Responses WebSocket passthrough/continuation affinity was already more integrated with ZyRealm's runtime than a generic raw proxy;
- the real gaps were upstream raw SSE liveness/commitment semantics and incomplete same-format coverage for OpenAI Chat Completions.

P1P was deliberately implemented in two narrow runtime PRs rather than as a broad proxy rewrite.

### P1P.1–P1P.3 — SSE commitment hardening via PR #30

PR #30 separated raw transport activity from semantic provider payload while preserving raw bytes and the existing routing/replay model:

- a deterministic RED proved upstream `: keepalive` comments could previously satisfy the wrong commitment signal: CI `35036654522`;
- implementation added the bounded incremental `SSEPayloadObserver` / payload-observer path rather than a stateless raw-chunk prefix check;
- comment-only liveness no longer stops TTFT or establishes semantic delivery;
- substantive `data:` events establish semantic delivery;
- replay safety, failure scope/cooldown/circuit behavior, typed routing decisions, and the historical Inspector remain unchanged;
- final PR-head GREEN: CI `35037353441`;
- squash merge commit: `9002439d76ec178d539c0ec77d41803761672544`;
- merged-tree CI: `35037506819` — governance, backend Vet/full tests, and frontend lint/test/build all success.

### P1P.4 — OpenAI Chat -> Chat same-format passthrough via PR #31

PR #31 extended the existing `PassthroughCapable` execution mode rather than adding a second relay subsystem:

- initial RED `35047865171` proved `ChatOutbound` lacked the optional passthrough capability;
- first GREEN `35048039449` established same-format raw forwarding;
- duplicate-model RED `35048544996` exposed a parser-dependent ambiguity when a later duplicate top-level `model` disagreed with the control-plane-selected model;
- final implementation normalizes all top-level `model` values when needed while never touching nested `model` fields;
- with one unambiguous top-level `model` already equal to the selected upstream model, raw request bytes remain byte-identical;
- unknown/future request and response fields survive the same-format relay;
- query parameters, `ContentLength`, `GetBody`, credential-owned upstream authorization, header policy, routing, replay, metrics, and Inspector remain on existing ZyRealm machinery;
- cross-format Chat routes remain on the standard transformer path;
- final PR head: `130a4c71ee5ea1140a58d7989e1fafd2421e15aa`;
- final PR-head CI: `35048893875` — governance, backend Vet/full tests, and frontend lint/test/build all success;
- scope audit: exactly four files changed — the P1P plan, one relay acceptance test, one Chat passthrough implementation file, and one adapter test file;
- no DB migration, dependency, routing schema, timeout/replay-policy, frontend runtime, deploy/compose, or production-state change;
- review audit: no inline review threads and no submitted reviews at the final head;
- squash merge commit: `5b773f13f84734eb222ad99e35b5840c69d2f188`;
- merged-tree CI: `35051237424` — governance, backend Vet/full tests, and frontend lint/test/build all success;
- deployment status: **not deployed** by this merge workflow.

P1P is therefore closed on the verified runtime baseline `main@5b773f13f84734eb222ad99e35b5840c69d2f188`.

### Post-P1P execution order

1. **Embeddings/Images passthrough source audit — NEXT.** Audit OpenAI embedding paths and the separate image relay/multipart surface against post-P1P `main`; do not automatically copy the reference endpoint matrix.
2. **P1C — control-plane conveniences.** Re-audit tri-state parameter/header override semantics and a privileged diagnostic direct-route. Generic HTTP sticky routing remains deferred.
3. **Only then re-evaluate schema-heavy P1.** Credential × model × protocol capability schema/migration work remains unapproved until fresh post-P1P evidence shows it is still necessary.

Use the repository's existing governance and CI contract for every successor slice. No production deployment is implied by this roadmap.

## P1 — Credential × model × protocol capability model — DEFERRED / RE-EVALUATE AFTER POST-P1P AUDITS

Remembered idea only: Octopus's `ChannelKey` / `ChannelModel` / `ChannelGrant` split is a useful authorization-modeling reference. ZyRealm already has a more advanced runtime credential pool, so any future P1 work must layer capability metadata onto the existing scheduler rather than replace it.

No schema, migration, API, or runtime design is approved here. P1 remains unapproved until the Embeddings/Images and P1C successor audits plus an operational-need assessment show that capability modeling is still necessary.

## Explicit non-goals

- No wholesale merge/rebase from Octopus or octopus-customization for these features.
- No second passthrough subsystem; same-format coverage extends the existing `PassthroughCapable` architecture.
- No P1 database migration without a fresh post-P1P source audit and explicit approval.
- No replacement of ZyRealm credential fairness, cooldown, circuit breaking, failure-domain classification, protocol fallback, replay-safety, live-request/manual-interrupt, or Routing Inspector logic with a simpler routing model.
- No blanket `if error then switch provider` handling that ignores dispatch uncertainty or downstream commitment.
- No broadening of first-token timeout duration, replay allowance, or attempt budgets as part of passthrough hardening.
- No generic sticky-session or public `channel/model` direct-route syntax in P1P.
- No dependency addition merely to mimic a reference implementation when existing ZyRealm code can satisfy the contract.
- No speculative planning of later slices based on an old file layout; each successor slice must be planned against the then-current `main`.

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
- [x] P0.2 merged/stabilized on `main`; final closure commit `8c629cddb1b41e28322950736b8089d6c19a6dcf`, merged-tree CI `34931539402` all green.
- [x] P0.3 source audit + detailed design/plan completed against current `main`.
- [x] P0.3 implemented with TDD and PR-head full regression verification.
- [x] P0.3 final implementation diff/review boundary audited; no unresolved review threads at reviewed head.
- [x] P0.3 merged/stabilized on `main`; squash merge `c60ba21cebe5d3b9424d2fffff97964e2b281deb`, merged-tree CI `34960012680` all green.
- [x] Historical Routing Inspector #27 accounted for as runtime baseline; `main@1f4b856b350c3941000e1ca9af12c7ed42f67622`, CI `35006072380` green.
- [x] P1P source audit completed across passthrough, TTFT, stream commitment, replay, parameter override, governance, CI, and pre-push paths.
- [x] P1P detailed implementation plan recorded.
- [x] P1P.1 RED reproduced upstream passthrough SSE comment-only liveness incorrectly satisfying first-token semantics; CI `35036654522`.
- [x] P1P.2 semantic stream-commitment hardening implemented with GREEN; PR #30 final CI `35037353441`, merge `9002439d76ec178d539c0ec77d41803761672544`, merged-tree CI `35037506819`.
- [x] P1P.3 replay/routing-decision/Inspector invariants verified under full repository CI and merged-tree validation.
- [x] P1P.4 OpenAI Chat same-format raw passthrough implemented with unknown-field, duplicate-model, replay-metadata, query, and credential-isolation coverage.
- [x] P1P final implementation PR head `130a4c71ee5ea1140a58d7989e1fafd2421e15aa` passed governance/backend/frontend CI `35048893875`; four-file scope and review-thread boundary audited.
- [x] P1P merged/stabilized on `main@5b773f13f84734eb222ad99e35b5840c69d2f188`; merged-tree CI `35051237424` all green.
- [ ] Embeddings/Images passthrough successor audit completed against post-P1P `main`.
- [ ] Re-evaluate P1C tri-state override/direct-route needs against post-P1P `main`.
- [ ] Re-evaluate whether schema-heavy P1 capability modeling is still necessary after post-P1P operational evidence.