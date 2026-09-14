# P0.1H Relay Failover Error Audit and Hotfix Plan

> Status: **MERGED + VERIFIED**. The production first-token-timeout failure was diagnosed from sanitized runtime evidence, reproduced deterministically at the real HTTP handler boundary, fixed with TDD, merged through PR #19, and re-verified on the merged `main` tree.

**Goal:** A first-token timeout before real downstream model payload commitment must advance through ZyRealm's existing bounded failover machinery when another eligible provider exists. True client cancellation, downstream payload commitment, replay-safety exhaustion, attempt-budget exhaustion, and candidate exhaustion remain terminal.

**Implementation baseline:** `main@c3e1e5a018b136ee61c7e2696275ea314f916dcd`.

**Implementation branch:** `codex/first-token-timeout-heartbeat-failover`.

**Merged runtime commit:** `12b3a6952739fac85f678002f0d0e8c77190e446`.

**Architecture:** Keep `RoutingDecision`, `DispatchState`, replay safety, request-local attempt budgets, runtime availability, outlier/circuit accounting, and the balancer iterator as the single routing path. Do not add a second retry engine or a blanket error-string switch.

## Production evidence

Sanitized read-only production evidence is preserved in:

- [`../debug/2026-09-14-relay-failover-diagnostic.md`](../debug/2026-09-14-relay-failover-diagnostic.md)
- [`../debug/2026-09-14-relay-failover-diagnostic.jsonl`](../debug/2026-09-14-relay-failover-diagnostic.jsonl)

Diagnostic PR #18 used deployed runtime `cbe4638b01aa5beb1a46f73dfb41cabaecaf890c` and covered 12 requests / 18 real attempts. The relevant evidence was:

1. Three first-token-timeout incidents had no real model payload before timeout, yet the trace ended as `downstream_committed` / terminal. The common predecessor was early SSE heartbeat/header output.
2. Three final `context canceled` incidents had authoritative outer request cancellation and were correctly terminal. These were not provider-health failures.
3. Two `INTERNAL_ERROR; received from peer` stream failures happened after real model protocol payload had already been delivered and were correctly terminal. They do not justify blind cross-provider replay.
4. Other transport/HTTP failures in the same evidence set demonstrated that provider failover itself was functioning; the failure was specific to the commitment boundary, not a globally broken iterator.

The evidence therefore satisfied the original runtime gate and ruled out a blanket `if error then switch` fix.

## Source audit result

The first-token timeout classifier was already correct on the implementation baseline:

- `RuleID = first_token_timeout`;
- failure scope is provider/model;
- routing action is next provider;
- the current provider is skipped;
- `MAYBE_SENT` becomes an unknown-upstream-outcome replay and remains bounded by the existing cross-provider replay allowance;
- real downstream commitment remains terminal.

The defect was downstream of timeout recognition.

## Root cause

For HTTP streaming attempts, delivery commitment must mean **provider payload forwarded downstream**, not merely **some bytes written by infrastructure**.

The early SSE heartbeat writes HTTP 200 headers and `:\n\n` before the upstream produces model output. That makes the generic Gin response writer report `Written() == true`. The previous `deliveryStarted()` fallback treated that transport-level state as if model output had been committed, so a first-token timeout that should have remained failover-eligible became `downstream_committed` and terminal.

This was a commitment-semantics bug, not a missing timeout classifier.

## Deterministic handler reproduction

The runtime evidence was followed by a deterministic integration test using the real relay `Handler` and timeout/failover loop:

1. failover group has provider A followed by provider B;
2. streaming heartbeat is enabled;
3. provider A waits long enough for the early heartbeat to be emitted but never emits real model payload before the first-token timeout;
4. provider B emits a valid SSE payload;
5. the test asserts A is attempted once, B is attempted once, and B's payload reaches the response.

The RED state reproduced the production failure: heartbeat-only output caused the first attempt to look committed and provider B was never called.

The final regression is `TestHandlerFirstTokenTimeoutFailsOverAfterEarlyHeartbeat` in `internal/relay/first_token_heartbeat_failover_test.go`.

## Minimal correction

`internal/relay/relay_attempt.go` now uses protocol-appropriate commitment evidence:

- WebSocket/custom stream writer: commitment follows the stream writer's real downstream event state;
- HTTP streaming: commitment follows `streamPayloadWritten`, not the generic Gin writer merely being written by heartbeat infrastructure;
- non-stream HTTP: ordinary response-writer commitment remains authoritative.

This preserves the existing post-output safety invariant while allowing pre-model-payload timeouts to continue through the normal failover path.

No timeout duration, retry count, routing taxonomy, replay allowance, provider/model/credential failure scope, circuit policy, or cooldown policy was widened.

## Explicit failover stop reasons

A retryable-looking attempt can still terminate later for a valid reason. To make that visible without a schema migration, the existing serialized `AttemptRoutingTrace` now has optional `failover_stop_reason` with a bounded vocabulary:

- `downstream_committed`
- `client_canceled`
- `no_alternative`
- `unknown_replay_budget`
- `wire_attempt_budget`
- `provider_attempt_budget`
- `candidate_exhausted`

The stop reason is late-bound to the existing attempt span when a later gate resolves. The first concrete reason wins, so a precise reason such as `no_alternative` is not overwritten by a generic request-finalization reason such as `candidate_exhausted`.

This is observability only; it is not a second routing-decision taxonomy.

## TDD evidence

The implementation was built in RED/GREEN slices.

- Delivery commitment RED proved that heartbeat/header-only streaming output incorrectly counted as model delivery.
- Real-handler RED proved that after early heartbeat + first-token timeout the fallback provider received zero requests.
- The minimal commitment correction turned both GREEN while retaining non-stream commitment semantics.
- Stop-reason RED covered `unknown_replay_budget` and `no_alternative` before trace plumbing existed.
- A second RED set covered `downstream_committed`, `client_canceled`, `wire_attempt_budget`, `provider_attempt_budget`, and `candidate_exhausted`.
- A preservation test locks "first concrete reason wins".
- Outer-loop cancellation RED proved that cancellation after a prior `next_provider` decision but before the next provider was not visible in the prior attempt trace; the existing cancellation gate now late-binds `client_canceled` without changing cancellation behavior.
- Existing committed-stream, empty-stream failover, WebSocket, and cancellation tests remained part of the full Go suite.

## Reference-repository audit

The current `bestruirui/octopus` relay handler was checked before finalizing the change. Its upstream runtime is materially simpler and does not have ZyRealm's `AttemptRoutingTrace`, request-local replay budget, failure-domain policy, or equivalent late terminal gates.

There was no mature handler patch that could be copied safely verbatim. The reusable idea was explicit request/termination state; ZyRealm applies that idea through its own existing `RoutingDecision` + `AttemptSpan` model rather than importing a second state machine.

## Verification evidence

Final implementation PR:

- PR: `#19 fix(relay): fail over after heartbeat-only first-token timeout`;
- final PR head: `76346f581421b0f9393e59b77c20c40ae8e1e676`;
- final PR-triggered GitHub Actions run: `34849018591`;
- governance: success;
- backend Vet: success;
- backend full `go test -buildvcs=false ./...`: success;
- frontend lint/test/build: success;
- no unresolved review thread at merge time.

Merged-tree verification:

- squash merge commit: `12b3a6952739fac85f678002f0d0e8c77190e446`;
- GitHub Actions run: `34860648315` (`CI #471`);
- governance: success;
- backend Vet + full Go tests: success;
- frontend lint + tests + production build: success.

## Scope boundary

This hotfix intentionally did **not** broaden into unrelated production-looking error families.

In particular, the audited HTTP/2 peer `INTERNAL_ERROR` samples were post-payload failures, where terminal behavior is correct. P0.1H therefore does not add a broad `stream read error` / `INTERNAL_ERROR` / `received from peer` substring classifier. A future pre-output peer-stream event may be investigated separately if real evidence demonstrates it.

Real outer-client cancellation remains terminal. Unknown-upstream-outcome replay remains bounded. No database migration, dependency change, frontend feature, deployment script, P0.2, P0.3, or P1 implementation is part of this slice.

## Completion checklist

- [x] Source-audit first-token timeout emission, classification, replay safety, attempt budgets, and terminal gates.
- [x] Confirm implementation baseline `main@c3e1e5a018b136ee61c7e2696275ea314f916dcd` did not drift during development.
- [x] Collect and sanitize production routing evidence for timeout, cancellation, and post-payload peer-stream failures.
- [x] Preserve diagnostic evidence without secrets, request bodies, or private upstream addresses.
- [x] Confirm the first-token-timeout defect from runtime evidence.
- [x] Build a deterministic real-handler reproduction of heartbeat + first-token-timeout failure.
- [x] Prove RED before the delivery-commitment correction.
- [x] Apply the smallest commitment-signal correction without changing replay policy.
- [x] Add bounded failover stop-reason observability to the existing JSON trace with no migration.
- [x] Add regression coverage for terminal gates, precedence, and late-bound cancellation.
- [x] Pass final PR governance, backend Vet/full tests, and frontend lint/test/build.
- [x] Complete final PR diff/review-thread boundary audit.
- [x] Squash merge PR #19 to `main`.
- [x] Pass merged-tree CI `34860648315` on `main@12b3a6952739fac85f678002f0d0e8c77190e446`.
- [x] P0.1H closed. P0.2 may begin only with a fresh source audit and detailed plan.
