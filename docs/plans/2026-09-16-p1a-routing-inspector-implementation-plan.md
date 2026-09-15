# P1A Routing Inspector Implementation Plan

> **For implementation agent:** execute this plan on a fresh runtime topic branch from then-current `main`; do not implement runtime code on the planning branch. Use test-first development and preserve every routing/failover semantic unless a test proves an existing defect.
>
> Design: [`2026-09-16-p1a-routing-inspector-design.md`](./2026-09-16-p1a-routing-inspector-design.md)
>
> Source-audit baseline used to write this plan: `main@a84de967172b86daa0b5fcc330654fee4dd43561`.

## Objective

Implement P1A.1 as a historical Routing Inspector for real requests:

```text
capture missing pre-dispatch decisions
        -> persist typed nested decision ledger in RelayLog.Attempts JSON
        -> build historical explanation from persisted facts only
        -> expose authenticated GET /api/v1/log/:id/routing
        -> render compact Routing Inspector in existing Log Detail UI
```

No database migration, route simulator, scheduler rewrite, live-history registry, raw payload feature, or deployment is part of this plan.

## Execution rules

1. Before implementation, re-fetch `main`; if runtime routing files changed after `a84de967...`, repeat the narrow source audit and update this plan before coding.
2. Create a fresh branch such as `feat/p1a-routing-inspector` from the audited `main`; do not reuse `codex/post-p0-gateway-operability-roadmap` for runtime changes.
3. The local Mac is edit/git only. **Do not run Go, pnpm, npm, build, test, vet, or install commands locally.**
4. Test execution is allowed only in GitHub CI or the fixed project server/container path already authorized by repository governance.
5. For each behavior slice, write the focused failing test first. Capture RED evidence through CI/server execution, then write the minimum implementation and capture GREEN evidence before expanding scope.
6. Do not merge merely because the topic branch CI is green. Review changed files, unresolved threads, diff scope, and merged-tree CI separately.
7. Merge does not authorize deployment.

## Task 0 — Fresh source pin and branch gate

**Read/recheck:**

- `AGENTS.md`
- `docs/plans/2026-09-16-p1a-routing-inspector-design.md`
- `internal/model/log.go`
- `internal/model/attempt_routing_trace.go`
- `internal/relay/balancer/iterator.go`
- `internal/relay/balancer/runtime_candidates.go`
- `internal/relay/credential_fair.go`
- `internal/relay/capability_cache.go`
- `internal/relay/relay_handler_support.go`
- `internal/relay/metrics.go`
- `internal/server/handlers/log.go`
- `web/src/api/endpoints/log.ts`
- `web/src/components/modules/log/Item.tsx`

**Gate:**

- confirm `main` SHA and record it in the implementation PR description;
- confirm no DB migration is required because the new ledger remains inside the existing `RelayLog.Attempts` JSON serializer;
- confirm `TotalAttempts = len(attempts)` still holds, because attempt-cardinality preservation is a hard invariant;
- confirm site-model attempt accounting still counts only `success`/`failed` attempts.

**Expected diff at this task:** no code yet.

---

## Task 1 — Define the typed routing-decision event contract

**Files:**

- Create: `internal/model/routing_decision_event.go`
- Modify: `internal/model/attempt_routing_trace.go`
- Create: `internal/model/routing_decision_event_test.go`

### Step 1.1 — RED: JSON contract tests

Write focused tests that require:

- enum/string values for stage, outcome, and reason;
- bounded structured fields only;
- `decision_events` omitted when empty;
- event JSON round-trip preserves ordering/sequence;
- no credential secret/header/request-content fields exist in the model contract.

The initial vocabulary should implement only audited reasons:

```text
stage:
  candidate | credential | protocol | dispatch | failover

outcome:
  eligible | rejected | selected | attempted | stopped

reason:
  runtime_cooldown
  runtime_suspect
  credential_cooldown
  capability_negative
  circuit_break
  capacity
  rate_limit
  protocol_incompatible
  attempt_budget
  replay_unsafe
```

Confirm the test fails because the contract does not exist.

### Step 1.2 — GREEN: minimal model

Add `RoutingDecisionEvent` plus small typed string aliases/constants. Add:

```go
DecisionEvents []RoutingDecisionEvent `json:"decision_events,omitempty"`
```

to the existing routing trace envelope rather than adding a RelayLog DB column.

Keep the event free-form detail optional and bounded by callers; the reason code is the machine contract.

### Step 1.3 — Verify

Run the focused model test in CI/server, then the backend test suite gate.

**Commit target:** `feat(relay): define routing decision event contract`

---

## Task 2 — Add a request-local decision collector without changing attempt semantics

**Files:**

- Modify: `internal/relay/balancer/iterator.go`
- Create: `internal/relay/balancer/iterator_decision_test.go`
- If helper extraction is cleaner, create: `internal/relay/balancer/decision_trace.go`

### Step 2.1 — RED: cardinality/idempotence tests

Write tests proving all of these invariants:

1. recording decision events does not increment `Iterator.count`;
2. recording decision events does not change `Len`, `Next`, candidate order, sticky state, or skipped-provider state;
3. with one existing real/skipped attempt, `Attempts()` returns the same one top-level record and same `AttemptNum`, with the ledger nested exactly once;
4. repeated `Attempts()` calls are idempotent and do not duplicate events;
5. returned snapshots cannot mutate the Iterator's internal ledger accidentally;
6. with no attempts and no decision events, `Attempts()` remains empty;
7. with no attempts but a non-empty ledger, `Attempts()` returns one explicit `AttemptSkipped` envelope with `AttemptKind == "decision_only"` and no provider/wire-attempt increment.

### Step 2.2 — GREEN: collector and materialization

Add a request-local ledger to `Iterator`, for example:

```go
decisionEvents []model.RoutingDecisionEvent
```

Add a side-effect-free method such as:

```go
func (it *Iterator) RecordDecision(event model.RoutingDecisionEvent)
```

Assign stable sequence numbers at record time.

Change `Attempts()` to materialize a copy:

- copy `it.attempts`;
- copy the decision-event slice;
- attach events to one deterministic existing envelope when attempts exist;
- otherwise create exactly one `decision_only` skipped envelope;
- never mutate `count` or the scheduler's internal attempt slice during materialization.

### Step 2.3 — Regression lock

Add/extend tests around `finalChannel`, `finalModel`, and `StatsSiteModelHourlyRecordAttempts` as necessary so a `decision_only` skipped envelope:

- is ignored for final provider/model choice;
- does not count as success or failed site-model traffic.

Likely files if new assertions are needed:

- `internal/relay/metrics_test.go`
- `internal/op/stats_site_model_test.go` (create only if there is no suitable existing test file)

**Commit target:** `feat(relay): persist request-local routing decision ledger`

---

## Task 3 — Capture provider/provider-model runtime filtering

**Files:**

- Modify: `internal/relay/balancer/runtime_candidates.go`
- Modify: `internal/relay/balancer/iterator.go`
- Create or extend: `internal/relay/balancer/runtime_candidates_test.go`

### Step 3.1 — RED: runtime rejection visibility

Write table-driven tests for at least:

- available candidate remains executable;
- half-open candidate remains in the normal pool exactly as today;
- suspect candidate remains in the fallback tier exactly as today;
- active cooldown candidate is not executable but yields a typed `candidate / rejected / runtime_cooldown` snapshot;
- the configured balancer receives exactly the same primary/suspect item sets as before instrumentation;
- instrumentation does not acquire a half-open lease.

### Step 3.2 — GREEN: return ordered candidates plus observational metadata

Refactor `runtimeOrderedCandidates` only enough to expose the decision metadata observed while applying the existing filter. Prefer a small internal result type such as:

```go
type runtimeCandidateOrder struct {
    Candidates []model.GroupItem
    Decisions  []model.RoutingDecisionEvent
}
```

`NewIteratorWithPreference` should initialize the iterator with the exact same executable candidate order and append the returned decision events to the request-local ledger.

Do not move `AcquireCandidate`, fairness, circuit, or admission semantics into this function.

### Step 3.3 — Verify selection equivalence

Existing health-order/round-robin/sticky tests plus new runtime-candidate tests must show no selection behavior drift.

**Commit target:** `feat(relay): trace runtime candidate exclusions`

---

## Task 4 — Capture credential runtime exclusions without changing fairness

**Files:**

- Modify: `internal/relay/credential_fair.go`
- Modify: `internal/relay/relay_handler_support.go`
- Extend existing credential tests or create: `internal/relay/credential_decision_test.go`

### Step 4.1 — RED: credential exclusion and fairness equivalence

Create tests with multiple keys where:

- one key is runtime-unavailable/cooling down;
- one or more keys remain eligible;
- the unavailable key yields `credential / rejected / credential_cooldown`;
- the exact surviving candidate set passed to fair selection is unchanged;
- the selected key is the same as current behavior;
- an already circuit-broken key keeps the existing `circuit_break` top-level behavior and is not double-recorded as a duplicate semantic event;
- already excluded key IDs do not create misleading new cooldown events.

### Step 4.2 — GREEN: pass an event sink, not a new selector

Do not reimplement credential selection. Extend the current function boundary so the existing loop can record a typed event at the same point where `CredentialAvailableRevision` causes `continue`.

A narrow callback or iterator pointer is acceptable if it keeps dependencies one-way. Example intent:

```go
selectFairChannelCredential(..., recordDecision func(model.RoutingDecisionEvent))
```

or use the existing iterator if that produces a cleaner dependency.

Only persist runtime metadata that is available from a read-only snapshot at decision time. Do not re-query current credential state later in the explanation builder.

### Step 4.3 — Verify no fairness side effects

Run the existing credential fairness tests plus the new focused tests. The number/order of calls into `availability.SelectCredentialFair` and the survivor IDs must not change.

**Commit target:** `feat(relay): trace credential runtime exclusions`

---

## Task 5 — Capture partial capability-negative protocol filtering

**Files:**

- Modify: `internal/relay/capability_cache.go`
- Modify: `internal/relay/relay_handler_support.go`
- Extend: `internal/relay/capability_cache_test.go` or the repository's current capability-negative test file

### Step 5.1 — RED: partial-filter regression test

Lock down the missing case:

```text
Plan A: OpenAI Responses      -> blocked by exact capability-negative evidence
Plan B: Chat Completions      -> eligible and survives
```

Assert:

- returned executable plans are identical to current behavior (`Plan B` only);
- a typed `protocol / rejected / capability_negative` event exists for Plan A;
- event carries only bounded plan/model/protocol metadata and cache expiry/reason if already present in the read-only `CapabilitySnapshot`;
- recording does not create, refresh, or clear capability-negative evidence;
- all-blocked behavior still produces the current skip/failure behavior.

### Step 5.2 — GREEN: structured filter result

Replace the current lossy `(filtered, nearest, blocked)` shape with a small internal structured result that can retain per-blocked-plan snapshots while leaving filtering semantics unchanged.

For example:

```go
type capabilityFilterResult struct {
    Plans   []*protocolroute.AttemptPlan
    Blocked []capabilityBlockedPlan
    Nearest availability.CapabilitySnapshot
}
```

`selectChannelAttempt` records one typed decision event per blocked plan before returning surviving plans.

Do not change capability signature/fingerprint, TTL, max entries, learning, or success-clear semantics.

**Commit target:** `feat(relay): trace capability-negative plan exclusions`

---

## Task 6 — Build a pure historical RoutingExplanation projection

**Files:**

- Create: `internal/relay/routing_explanation.go`
- Create: `internal/relay/routing_explanation_test.go`

### Step 6.1 — RED: projection contract tests

Build fixture-only tests; no availability/global runtime setup should be required.

Cover:

1. one successful attempt with candidate/credential/protocol decisions;
2. failed provider/model attempt followed by safe reroute and success;
3. circuit/capacity/rate existing attempt statuses;
4. protocol fallback using existing `SelectedProtocol`, `AttemptKind`, `FallbackReason` fields;
5. failure-domain/rule/retry/runtime/replay/failover-stop fields from `AttemptRoutingTrace`;
6. `decision_only` all-filtered request;
7. old log with no decision ledger -> `legacy_partial` rather than error;
8. no raw request/response content appears in the DTO.

### Step 6.2 — GREEN: pure builder

Implement a function with no process-global reads, for example:

```go
func BuildRoutingExplanation(log model.RelayLog) RoutingExplanation
```

The DTO should include at minimum:

- schema/version;
- completeness (`complete`, `legacy_partial`, `decision_only`);
- requested model;
- bounded final route summary;
- ordered explanation steps;
- stable stage/outcome/reason fields for UI rendering.

Do not call `availability`, `balancer`, channel configuration, capability cache, or credential runtime from this builder.

Avoid duplicating semantic steps when the same fact already exists in a top-level attempt and the nested ledger.

**Commit target:** `feat(relay): build historical routing explanations`

---

## Task 7 — Expose authenticated routing explanation API

**Files:**

- Modify: `internal/server/handlers/log.go`
- Create: `internal/server/handlers/log_routing_test.go` if handler-test conventions support it; otherwise extend the closest existing authenticated log-handler test.

### Step 7.1 — RED: handler contract

Test:

- invalid ID -> existing bad-parameter behavior;
- missing log -> existing not-found behavior;
- valid log -> explanation DTO;
- old log -> `legacy_partial` response;
- route remains under `middleware.Auth()`;
- response excludes request/response bodies and credential secret values.

### Step 7.2 — GREEN: thin endpoint

Add:

```text
GET /api/v1/log/:id/routing
```

inside the existing authenticated log router.

Implementation must only:

1. parse ID;
2. call existing `op.RelayLogGet`;
3. call `relay.BuildRoutingExplanation`;
4. return with existing response helpers.

Do not recompute candidates from current configuration/runtime state.

**Commit target:** `feat(api): expose routing explanation endpoint`

---

## Task 8 — Add frontend API types without changing existing log-card semantics

**Files:**

- Modify: `web/src/api/endpoints/log.ts`
- Create or extend frontend contract test: `web/tests/routing-inspector-ui.test.mjs`

### Step 8.1 — RED: source/API contract test

Following the current lightweight `node:test` convention, assert the frontend defines the routing explanation types and fetches:

```text
/api/v1/log/${id}/routing
```

Also assert `ChannelAttempt` is expanded to include the backend routing trace fields needed by the current diagnostics, without removing existing fields/status values.

### Step 8.2 — GREEN: API contract

Add typed interfaces for:

- decision event;
- explanation step;
- final route/summary as required by backend DTO;
- completeness/version;
- `getRoutingExplanation(id)` or a focused query hook.

Keep normal log list payloads lightweight; do not force explanation data into every `/log/list` response.

**Commit target:** `feat(web): add routing explanation client contract`

---

## Task 9 — Add Routing Inspector to existing Log Detail

**Files:**

- Create: `web/src/components/modules/log/RoutingInspector.tsx`
- Modify: `web/src/components/modules/log/Item.tsx`
- Extend: `web/tests/routing-inspector-ui.test.mjs`

### Step 9.1 — RED: UI behavior contract

Test/source-contract expectations:

- Inspector is loaded from log detail, not the global log list;
- compact steps render stage/outcome/reason labels;
- `legacy_partial` is visibly marked as partial rather than treated as complete;
- `decision_only` is not rendered as a wire retry;
- existing `RetryBadgeWithTooltip` and top-level `attempts.length` behavior are not modified to count nested events;
- no raw request/response body is passed into the Inspector.

### Step 9.2 — GREEN: focused component

Render the explanation in the current diagnostic/detail dialog rather than creating a new top-level navigation page.

Default presentation:

- requested model;
- rejected/eligible/selected route steps;
- selected provider/key as stable IDs/names already safe in logs;
- protocol selection/fallback;
- failure/reroute/commit outcome;
- final success/failure.

Put rule IDs, cooldown expiry, runtime state, replay safety, dispatch state, and other advanced fields behind compact expandable detail.

Use existing UI primitives and visual language. Do not add a new charting/UI framework.

**Commit target:** `feat(web): add routing inspector to log detail`

---

## Task 10 — Full regression and semantic non-interference gate

No new feature work after this point unless a failing regression requires correction.

### Required backend assertions

At minimum prove:

- provider order unchanged for available/suspect/half-open candidates;
- cooldown candidates remain non-executable;
- credential fairness result unchanged;
- capability-negative plan filtering unchanged;
- retry and failover directives unchanged;
- replay-safety and downstream-commit behavior unchanged;
- half-open lease behavior unchanged;
- attempt-budget indexes unchanged;
- existing top-level attempt count unchanged whenever the request already had attempts;
- `decision_only` does not count as success/failed site-model traffic;
- old persisted JSON deserializes successfully.

### Required frontend assertions

Prove:

- existing log list/card behavior still works;
- retry badge counts only top-level attempts;
- Routing Inspector handles complete, legacy-partial, and decision-only explanations;
- lint and production build pass.

### Cloud/server verification commands

These commands are evidence targets for GitHub CI or the fixed server/container environment only. **Do not run them on the local Mac.**

```bash
bash scripts/check-governance.sh --repo

go test -buildvcs=false ./...

cd web
pnpm lint
pnpm test
pnpm build
```

`go vet ./...` is currently advisory/continue-on-error in repository CI because of a pre-existing copylocks warning; capture the output but do not redefine the repository gate in P1A.

### GitHub Actions gate

The current CI workflow runs on pushes to `feat/**` and PRs targeting `main`. Require:

- governance: success;
- backend tests: success;
- frontend lint: success;
- frontend tests: success;
- frontend build: success.

Record the workflow run ID in the PR before review/merge.

**Commit target if only tests/docs are added:** `test(relay): lock routing inspector non-interference`

---

## Task 11 — Review and merge gate

Before marking the PR ready:

1. inspect changed filenames; expected scope is routing model/relay/log handler/log UI/tests/docs only;
2. confirm there is no DB migration, deployment script, production configuration mutation, or new dependency unless separately justified;
3. confirm no route simulator/dry-run implementation slipped into P1A.1;
4. confirm no new synthetic provider probes;
5. confirm no raw prompt/response or credential material in decision events/API DTO;
6. confirm no unresolved review threads;
7. re-run/confirm full CI on the final head SHA;
8. merge only after explicit review approval under the existing repository process;
9. verify merged-tree CI separately;
10. do not deploy unless separately authorized.

After merged-tree verification, update the parent roadmap from `P1A implementation pending` to `P1A MERGED + VERIFIED`, then re-evaluate P1B/P1C from operational evidence rather than starting either automatically.

## Explicit deferred work

The following are **not** implementation tasks in this plan:

- route simulation / dry-run;
- pure-planner refactor required for safe simulation;
- live candidate-history streaming;
- declarative capability hints (P1B);
- routing-health reset console (P1C);
- raw payload forensics (P2A);
- request/prompt transformation policy (P2B);
- deployment.
