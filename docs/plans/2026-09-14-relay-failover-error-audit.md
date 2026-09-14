# P0.1H Relay Failover Error Audit and Hotfix Plan

> Status: first-token-timeout defect reproduced deterministically at the real HTTP handler boundary, fixed with TDD, and fully CI-verified on the feature branch. A production incident trace was not available through the current repository tooling and is not fabricated here.

**Goal:** A first-token timeout before real downstream payload commitment must advance through ZyRealm's existing bounded failover machinery when another eligible provider exists. True client cancellation, downstream payload commitment, replay-safety exhaustion, attempt-budget exhaustion, and candidate exhaustion remain terminal.

**Baseline:** `main@c3e1e5a018b136ee61c7e2696275ea314f916dcd`.

**Implementation branch:** `codex/first-token-timeout-heartbeat-failover`.

**Architecture:** Keep `RoutingDecision`, `DispatchState`, replay safety, request-local attempt budgets, runtime availability, outlier/circuit accounting, and the balancer iterator as the single routing path. Do not add a second retry engine or a blanket error-string switch.

## Source audit result

The first-token timeout classifier was already correct on the baseline:

- `RuleID = first_token_timeout`;
- failure scope is provider/model;
- routing action is next provider;
- the current provider is skipped;
- `MAYBE_SENT` becomes an unknown-upstream-outcome replay and remains bounded by the existing cross-provider replay allowance;
- downstream commitment remains terminal.

The defect was therefore downstream of timeout recognition.

## Deterministic runtime reproduction

A production incident trace was not available from the GitHub connector. Instead of inventing one, the defect was isolated with a deterministic integration test that invokes the real `Handler` and real timeout/failover loop:

1. failover group has provider A followed by provider B;
2. streaming heartbeat is enabled;
3. provider A waits long enough for the early SSE heartbeat to be emitted but never emits a real model payload before the first-token timeout;
4. provider B emits a valid SSE payload;
5. the test asserts A is attempted once, B is attempted once, and B's payload reaches the response.

The RED reproduction showed that the early heartbeat had made the generic Gin writer look "written", which was being interpreted as real downstream response commitment. That converted an otherwise retryable first-token timeout into a terminal post-commit failure.

## Root cause

For HTTP streaming attempts, delivery commitment must mean **provider payload forwarded downstream**, not merely **some bytes were written by infrastructure**. The early heartbeat is out-of-band liveness and can occur before any provider payload.

The previous commitment check used the generic response-writer written state broadly enough that the heartbeat could set `DownstreamCommitted=true`. The routing decision then correctly refused replay, but for the wrong commitment signal.

## Minimal correction

`internal/relay/relay_attempt.go` now uses protocol-appropriate commitment evidence:

- WebSocket: committed only after a downstream event has actually been forwarded;
- HTTP streaming: committed only after a provider stream payload has actually been sent;
- non-stream HTTP: continue using the response writer's committed state.

This preserves the existing safety rule after real output while preventing an infrastructure heartbeat from suppressing legitimate failover.

No timeout duration, retry count, routing taxonomy, replay allowance, provider/model/credential failure scope, circuit policy, or cooldown policy was widened.

## Explicit failover stop reasons

A retryable-looking attempt can still terminate later for a valid reason. To make that visible without a schema migration, the existing serialized attempt routing trace now has optional `failover_stop_reason` with a bounded vocabulary:

- `downstream_committed`
- `client_canceled`
- `no_alternative`
- `unknown_replay_budget`
- `wire_attempt_budget`
- `provider_attempt_budget`
- `candidate_exhausted`

The stop reason is late-bound to the existing attempt span when a later gate resolves. The first concrete reason wins, so a precise reason such as `no_alternative` is not overwritten by a generic request-finalization reason such as `candidate_exhausted`.

## TDD evidence

The implementation was built in RED/GREEN slices.

- First stop-reason RED: `unknown_replay_budget` and `no_alternative` were both absent; minimal trace plumbing made them GREEN.
- Second stop-reason RED: `downstream_committed`, `client_canceled`, `wire_attempt_budget`, `provider_attempt_budget`, and `candidate_exhausted` were absent; gate-local late binding made them GREEN.
- A preservation test locks "first concrete reason wins".
- Outer-loop cancellation RED proved that cancellation after a prior `next_provider` decision but before the next provider was not visible in the prior attempt trace; the outer cancellation gate now late-binds `client_canceled` without changing cancellation behavior.
- Delivery-commitment tests lock the key invariant that heartbeat writes do not commit an HTTP stream, while non-stream HTTP still uses ordinary writer commitment.
- `TestHandler_FirstTokenTimeoutAfterEarlyHeartbeatFailsOver` exercises the real handler and proves provider A timeout -> provider B success after an early heartbeat.

## Reference-repository audit

The current `bestruirui/octopus` relay handler was checked before finalizing the change. Its upstream runtime is materially simpler and does not have ZyRealm's `AttemptRoutingTrace`, request-local replay budget, failure-domain policy, or equivalent late terminal gates. There is no mature handler patch that can be safely copied verbatim.

The reusable idea is the simpler upstream's explicit request/termination state: ZyRealm applies that idea through its own existing `RoutingDecision` + `AttemptSpan` model rather than importing a second state machine.

## Verification evidence

Verified code head before this documentation-only fold:

- head: `5692cc99be10334b14b7e11e02b2abc43f4c96de`;
- GitHub Actions run: `34845934726`;
- governance: success;
- backend Vet: success;
- backend full `go test -buildvcs=false ./...`: success;
- frontend lint/test/build: success.

This documentation commit creates a newer branch head, so a fresh all-green GitHub Actions run is still required before integration.

## Scope boundary

This hotfix intentionally does **not** broaden into the other production-looking error families discussed during audit. In particular, HTTP/2 peer `INTERNAL_ERROR` / stream-read marker classification remains a separate evidence-driven follow-up unless a pre-output event is proven. Real outer-client cancellation remains terminal.

No database migration, dependency change, frontend feature change, deployment script, P0.2, P0.3, or P1 implementation is part of this slice.

## Completion checklist

- [x] Source-audit first-token timeout emission, classification, replay safety, attempt budgets, and terminal gates.
- [x] Confirm baseline `main@c3e1e5a018b136ee61c7e2696275ea314f916dcd` did not drift during implementation.
- [ ] Capture a sanitized production incident trace. Not available in current tooling; intentionally not fabricated.
- [x] Build a deterministic real-handler reproduction of the observed heartbeat + first-token-timeout failure mode.
- [x] Prove RED before the delivery-commitment correction.
- [x] Apply the smallest commitment-signal correction without changing replay policy.
- [x] Add bounded failover stop-reason observability to the existing JSON trace, with no migration.
- [x] Add regression coverage for terminal gates and late-bound cancellation.
- [x] Pass full backend, frontend, and governance CI at code head `5692cc99be10334b14b7e11e02b2abc43f4c96de`.
- [ ] Pass fresh full CI on the final documentation-inclusive branch head.
- [ ] Final PR diff/review-thread boundary audit.
- [ ] Merge P0.1H before starting P0.2 source audit.
