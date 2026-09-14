# P0.1H Relay Failover Error Audit and Hotfix Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use systematic debugging before implementation, then test-driven development for each behavior change. Do not change production routing code until the runtime-evidence gate in Task 1 is satisfied.

**Goal:** Make retryable failures that occur before downstream response commitment advance through ZyRealm's existing failover machinery, while keeping true client cancellation and post-commit stream failures terminal.

**Architecture:** Do not add a second retry engine or string-only exception list. Keep `RoutingDecision`, `DispatchState`, replay safety, attempt budgets, runtime availability, outlier accounting, circuit policy, and the balancer iterator as the single routing path. First establish why production events that look retryable are ending terminal; then close only the proven classification/control-flow gaps and add an explicit traceable terminal reason when failover is intentionally blocked.

**Tech Stack:** Go 1.25 relay runtime, existing `internal/relay` routing/failure classifiers, balancer iterator, runtime availability, relay attempt trace, Go tests, GitHub Actions.

**Spec:** This document expands the queued P0.1H entry in `docs/plans/2026-09-14-octopus-gap-roadmap.md` after production observations on 2026-09-14.

## Global Constraints

- Base all work on `main@c3e1e5a018b136ee61c7e2696275ea314f916dcd` or a later verified `main`; record drift before changing code.
- Do not retry a request after downstream payload has been committed merely to hide an upstream stream failure.
- Do not treat a real outer-client cancellation/deadline as an upstream failure or retry it after the client is gone.
- For `MAYBE_SENT` failures, preserve the existing bounded unknown-outcome cross-provider replay safety; never turn it into unlimited replay.
- Reuse `RoutingDecision` and the existing iterator. Do not create a parallel failover state machine.
- Do not weaken provider/model/credential failure scopes or circuit/cooldown semantics without evidence.
- No database migration, new dependency, or deployment-script change is expected for this hotfix.
- Production logs committed to this public repository must be sanitized: no API keys, Authorization/Cookie values, request/response bodies, prompts, tool arguments, user data, or private upstream hosts/IPs.

---

## Observed failure families

Production UI showed several superficially similar terminal failures:

```text
failed to send request: first token timeout (20s/30s)
failed to send request: Post "<upstream>/v1/chat/completions": context canceled
stream read error: stream error: stream ID N; INTERNAL_ERROR; received from peer
```

They must not be collapsed into one rule.

### A. First-token timeout

Current `main` already recognizes this as a dedicated routing event:

- `RuleID = first_token_timeout`
- `FailureScope = provider_model`
- `Directive = next_provider`
- `SkipProvider = true`
- `RuntimeEffect = model_cooldown`
- `DispatchState == maybe_sent` becomes `ReplaySafety = unknown_upstream_outcome`
- downstream committed/reset becomes terminal.

Therefore the production symptom is **not sufficient evidence that the classifier is missing**. A terminal request can still be correct if no alternative remains, the bounded unknown-outcome replay allowance is exhausted, an attempt budget is exhausted, or the deployed binary is not this `main`.

### B. `context canceled`

Current `main` intentionally distinguishes two cases:

1. Outer request context is canceled/deadline-exceeded: this is a real client/proxy cancellation and is terminal.
2. Outer request is still active but the outbound/child transport returns `context.Canceled`/`DeadlineExceeded`: this is `ambiguous_transport_cancel`, is request-locally failover-eligible, and can move to the next provider subject to replay safety.

A string such as `Post "<upstream>": context canceled` alone cannot tell which case happened. The outer-context state at decision time is required.

### C. HTTP/2 / stream `INTERNAL_ERROR`

A stream read error must be split by downstream commitment:

- **Before any real payload is written:** treat a peer/transport stream failure as a failover candidate, subject to the ordinary budgets and routing policy.
- **After payload is written:** terminal is correct. Switching providers would splice or duplicate two independent generated responses and can corrupt the client-visible stream.

Current provider-transient marker coverage includes common connection reset/broken-pipe/TLS/timeout failures but does not explicitly name `stream read error`, `INTERNAL_ERROR`, or `received from peer`. This is a candidate classification gap, but it must be proven with an event where `DownstreamCommitted=false` before production code is changed.

## Current source trace

### First-token timeout propagation

- `internal/relay/first_token_timeout.go`: child request context receives `errFirstTokenTimeout`; helper normalizes transport cancellation back to a typed timeout and logs `switching channel`.
- `internal/relay/relay_request.go`: entering `httpClient.Do` sets `DispatchState = maybe_sent`.
- `internal/relay/relay_attempt.go`: failed attempts set `FirstTokenTimeout` and attach a unified `RoutingDecision`.
- `internal/relay/routing_decision.go`: first-token timeout selects `next_provider` unless downstream is already committed.

### Cancellation provenance

- `internal/relay/cancel.go`: only the outer request context is authoritative evidence of client cancellation.
- `internal/relay/routing_decision.go`: outbound cancellation with an active outer request becomes `ambiguous_transport_cancel` and `next_provider` unless committed.
- `internal/relay/ambiguous_cancellation_handler_test.go`: already verifies both `MAYBE_SENT` and `NOT_SENT` failover behavior.

### Why an apparently retryable event can still end terminal

`internal/relay/relay_handler.go` and `internal/relay/attempt_budget.go` impose additional gates after classification:

- no alternative provider remains;
- alternative candidates are all skipped by runtime cooldown, circuit, disabled state, capability negative cache, concurrency, or RPM;
- provider/wire attempt budget is exhausted;
- an unknown-outcome `MAYBE_SENT` cross-provider replay has already consumed the single balanced replay allowance (`defaultMaxUnknownCrossProviderReplay = 1`);
- downstream delivery has started;
- outer client context is actually canceled.

The diagnosis must identify which gate stopped each real incident.

### Existing routing trace fields

`AttemptRoutingTrace` already records enough provenance to avoid relying only on human-readable error strings:

- `FailureDomain`
- `FailureScope`
- `RuleID`
- `RetryDirective`
- `RuntimeEffect`
- `CircuitEffect`
- `OutlierEffect`
- `ReplaySafety`
- `DispatchState`
- `DownstreamCommitted`
- `OuterContextState`
- `OutboundContextCause`
- `ProviderAttempt`
- `WireAttempt`

Use these fields as the primary runtime evidence.

## Expected decision matrix

| Failure | Outer client | Downstream committed | Expected action |
| --- | --- | --- | --- |
| first-token timeout | active | false | next provider/candidate, bounded by unknown-outcome replay and attempt budgets |
| first-token timeout | active | true | terminal |
| outbound `context canceled` | active | false | ambiguous transport cancellation -> next provider/candidate, replay-safe bounds apply |
| outbound `context canceled` | canceled/deadline | false/true | terminal client cancellation, no upstream-health penalty |
| HTTP/2 `INTERNAL_ERROR` / peer stream failure | active | false | provider/transport failover candidate |
| HTTP/2 `INTERNAL_ERROR` / peer stream failure | active | true | terminal; never splice a replacement stream |
| any failure | active | false, but no eligible candidate/budget | terminal with an explicit no-failover reason |

## Task 1: Collect sanitized runtime evidence before changing routing code

**Files:**
- Create on a diagnostics branch: `docs/debug/2026-09-14-relay-failover-diagnostic.md`
- Optional structured companion: `docs/debug/2026-09-14-relay-failover-diagnostic.jsonl`
- Do not modify production Go files in this task.

**Interfaces:**
- Consumes: persisted/logged `ChannelAttempt` / `AttemptRoutingTrace` and actual deployed Git SHA.
- Produces: 2-5 sanitized examples for each observed failure family, with enough fields to explain whether failover was attempted or blocked.

- [ ] **Step 1: Confirm the deployed build SHA**

Record the exact deployed commit. If it differs from `c3e1e5a018b136ee61c7e2696275ea314f916dcd`, do not assume current-main behavior was present in the incident.

- [ ] **Step 2: Export only routing evidence for first-token timeout events**

For 2-5 events, record timestamp/log ID, model/group, channel/key numeric IDs, streaming flag, sanitized error, attempt order, all available `AttemptRoutingTrace` fields, alternative candidate/skip evidence, and final request outcome.

- [ ] **Step 3: Export only routing evidence for `context canceled` events**

The required discriminator is `OuterContextState` at the routing decision. Also record `OutboundContextCause`, `DispatchState`, `ReplaySafety`, and whether a next provider attempt exists.

- [ ] **Step 4: Export only routing evidence for `INTERNAL_ERROR` stream events**

The required discriminator is `DownstreamCommitted`. Also record whether any non-heartbeat payload/token had been written before the read error.

- [ ] **Step 5: Record candidate-exhaustion reasons**

For a terminal event that had `RetryDirective=next_provider`, record why another candidate did not run: no alternative, runtime cooldown, circuit, disabled channel, capability negative cache, concurrency, RPM, provider/wire budget, or unknown-replay budget.

- [ ] **Step 6: Sanitize and commit diagnostic evidence only**

Redact upstream hosts/IPs to `<upstream>` and remove secrets/bodies. If the evidence cannot be obtained from available logs/DB/API, write exact local collection instructions instead of inventing values.

## Task 2: Add regression tests for the proven failure path

**Files:**
- Modify: `internal/relay/routing_decision_test.go`
- Modify or extend: `internal/relay/ambiguous_cancellation_handler_test.go`
- Create when needed: `internal/relay/pre_output_transport_failover_test.go`
- Modify stream tests only if the diagnostic proves a pre-output stream-read classification gap.

**Interfaces:**
- Consumes: the exact terminal gate identified in Task 1.
- Produces: failing tests that reproduce the production behavior before implementation.

- [ ] **Step 1: Write a failing two-provider first-token-timeout integration test if runtime evidence shows current-main fails to advance**

The first provider must time out before downstream payload; the second provider must return success. Assert attempt order and routing trace, not just HTTP status.

- [ ] **Step 2: Write cancellation provenance tests for any uncovered path**

Assert `outer active + outbound canceled -> failover`, and `outer canceled/deadline -> terminal/no provider-health penalty`.

- [ ] **Step 3: Write pre-output peer-stream failure test if proven**

Inject a wrapped error representative of HTTP/2 peer `INTERNAL_ERROR` before any payload and assert that another provider remains eligible.

- [ ] **Step 4: Lock the post-output safety invariant**

Inject the same stream failure after payload commitment and assert terminal behavior with no second-provider replay.

- [ ] **Step 5: Run only the new tests and prove RED**

Use `go test -buildvcs=false ./internal/relay -run '<new test regex>' -count=1`. The failure must match the diagnosed production gap rather than a test setup error.

## Task 3: Implement the smallest routing/classification correction

**Files:**
- Exact production files depend on Task 1 evidence. Expected candidates are limited to:
  - `internal/relay/provider_failover.go` for a proven missing transport marker/classifier;
  - `internal/relay/routing_decision.go` for a proven decision gap;
  - `internal/relay/relay_handler.go` for a proven terminal-gate/control-flow defect;
  - `internal/relay/attempt_budget.go` only if evidence proves the current replay accounting, rather than classification, is the defect.

**Interfaces:**
- Consumes: RED tests and the recorded routing trace.
- Produces: the same unified `RoutingDecision` flow with corrected pre-output behavior.

- [ ] **Step 1: Change only the layer proven wrong by the diagnostic**

Do not add broad substring handling to unrelated paths. Preserve typed/context-aware checks where they already exist.

- [ ] **Step 2: Keep post-commit and real-client-cancel paths terminal**

No implementation may route around `DownstreamCommitted=true` or an authoritative canceled outer request.

- [ ] **Step 3: Preserve bounded unknown-outcome replay**

A `MAYBE_SENT` timeout/cancellation may cross providers only within the existing replay-safety budget unless a separate, explicitly reviewed policy change is justified by the diagnostic.

- [ ] **Step 4: Run the focused tests and prove GREEN**

Run the exact RED command from Task 2 and require all new tests to pass.

## Task 4: Make intentional no-switch outcomes observable

**Files:**
- Modify the smallest existing trace/log model needed after inspecting how `AttemptRoutingTrace` is persisted/rendered.
- Do not add a database migration merely for this task unless the existing serialized trace cannot carry a new optional field.

**Interfaces:**
- Consumes: current decision/iterator/budget state.
- Produces: an operator-visible reason when a retryable decision still becomes terminal.

- [ ] **Step 1: Define a bounded no-failover reason vocabulary**

Examples should correspond to real gates, such as `downstream_committed`, `client_canceled`, `no_alternative`, `unknown_replay_budget`, `wire_attempt_budget`, `provider_attempt_budget`, or `candidate_exhausted`.

- [ ] **Step 2: Record the reason without secrets or request bodies**

The routing trace/log should make `RetryDirective=next_provider` plus a later terminal gate explainable from one request's attempt history.

- [ ] **Step 3: Add tests for the reason field/output**

At minimum cover unknown-replay-budget exhaustion and no-alternative exhaustion.

## Task 5: Full verification and merge gate

**Files:**
- No additional feature scope.

- [ ] **Step 1: Run relay-focused tests**

```bash
go test -buildvcs=false ./internal/relay/... -count=1
```

- [ ] **Step 2: Run repository backend verification**

```bash
mkdir -p static/out
touch static/out/.keep
go vet ./...
go test -buildvcs=false ./...
```

- [ ] **Step 3: Run governance and frontend CI-equivalent gates**

```bash
bash scripts/check-governance.sh --repo
cd web
pnpm install --frozen-lockfile
pnpm lint
pnpm test
pnpm build
```

- [ ] **Step 4: Review final diff boundaries**

No P0.2/P0.3/P1 implementation, no unrelated refactor, no migration/dependency/deployment change unless independently justified and reviewed.

- [ ] **Step 5: Require GitHub Actions green before merge**

Do not mark the hotfix complete based only on local or partial tests.

## Audit conclusion before runtime evidence

Current source already has explicit failover logic for both first-token timeout and ambiguous outbound cancellation. Therefore the screenshot alone does **not** justify adding another blanket `if error then switch` rule. The likely unresolved area is either a later safety/exhaustion gate, deployment-version mismatch, or a pre-output stream-read transport classification gap. Task 1 is mandatory so the hotfix fixes the actual terminal gate instead of weakening replay safety.