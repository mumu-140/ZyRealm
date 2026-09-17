# Group Operational Health + Active Diagnostics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** stop conflating synthetic group probes with routing health. Make passive real-traffic health the primary Group health signal, retain active end-to-end probing only as an explicitly manual diagnostic, and guarantee diagnostic results never alter routing health/cooldown/circuit state.

**Architecture:** keep the existing `grouphealth` persistence/endpoints for diagnostic history, but change the product semantics/UI from “health check” to “active diagnostic”. Add a separate read-only Operational Health inspection surface sourced from the exact `outlierwindow` + balancer/circuit state already used by `HealthFirst`/Failover. Export an inspection snapshot from `internal/relay/balancer` so the control-plane API reuses the existing score/tier logic instead of copying it. Do not feed active diagnostic results back into runtime health.

**Tech Stack:** Go, Gin, GORM (existing diagnostic snapshots only), in-memory `outlierwindow`, relay balancer/circuit breaker, Next.js 16, TanStack Query, next-intl.

**Spec:** `docs/plans/2026-09-17-group-ui-auto-add-health-audit.md`

## Audit Facts That This Plan Must Preserve

1. The current synthetic probe makes a real provider request with literal `"ping"`, streaming off, and a one-token output budget.
2. Probe success is reachability/HTTP success; it does not require the model to answer `pong`.
3. Current `standard` vs `full` changes traversal, not request protocol. For Failover, standard stops after first success; full checks all candidates. Non-Failover standard already checks all candidates.
4. `GroupHealthSnapshot` / `GroupHealthAttempt` are diagnostic-history records only.
5. Runtime health used by routing is separate:
   - every real relay result reports into `outlierwindow`;
   - `itemHealthScore()` derives the 0–1 score from the rolling failure window;
   - `HealthFirst` uses score tiers `>=0.6`, `>=0.3`, `<0.3` and pushes circuit-tripped candidates to the bad tier;
   - Failover preserves Priority first, then uses circuit/health as secondary ordering.
6. Synthetic group probes currently do **not** report to `outlierwindow`, availability/cooldown, or the circuit breaker.

## Product Decision

Use two distinct terms and two distinct data paths:

- **Operational Health / 运行健康** — passive evidence from actual relay traffic. This is the authoritative health signal and is the only one allowed to affect route ordering/circuit behavior.
- **Active Diagnostic / 主动诊断** — manually initiated synthetic smoke test. It answers “can this provider/model respond to a tiny request right now?” and is informational only.

The Group card must no longer present the most recent synthetic probe snapshot as if it were the routing health score.

## Global Constraints

- No diagnostic result may call `outlierwindow.Report*`, `balancer.RecordSuccess/Failure`, credential availability/cooldown APIs, or circuit APIs.
- No automatic diagnostic scheduler is added.
- No DB migration or rename of existing `GroupHealthSnapshot` tables/endpoints in this slice; compatibility beats internal naming cleanup.
- Do not duplicate the `itemHealthScore()` formula or `healthTierOf()` thresholds in handlers/frontend.
- The Operational Health API is read-only and process-local, matching current runtime health semantics (it resets on process restart because the underlying window/circuit state does).
- UI must distinguish `no_data` from actual degraded evidence even though the routing score for zero samples is currently neutral `0.5`.
- A failed active diagnostic must never automatically label Operational Health unhealthy.
- No local Mac build/install/deploy.

---

## Delivery Shape

Ship as two reviewable PRs after the Group UI stability work:

### PR A — Active Diagnostic semantic correction

- rename/de-emphasize synthetic “health” UI;
- replace recognizable `ping` probe content with a tiny ordinary request;
- collapse the two primary “health/full” actions into one Diagnostic action with an explicit “check all candidates” option;
- lock the no-routing-side-effect dependency boundary.

### PR B — Operational Health inspection

- expose the exact real-traffic item health snapshot already used by the balancer;
- add one page-level Group Operational Health query;
- use it as the primary Group card health badge/detail;
- retain Active Diagnostic as a secondary/manual tool.

Do not combine PR A and PR B if PR A is ready first.

---

# PR A — Active Diagnostic Semantic Correction

## File Structure

**Create**
- `internal/grouphealth/dependency_boundary_test.go` — forbid routing-health imports/side effects from the diagnostic package.
- `web/src/provider/group-diagnostic-messages.ts` — focused `en` / `zh_hans` / `zh_hant` copy.
- `web/tests/group-diagnostic-semantics.test.mjs` — frontend/source contracts.

**Modify**
- `internal/grouphealth/probe.go` — tiny ordinary diagnostic payload and two-token budget.
- `internal/grouphealth/probe_test.go` — exact payload/protocol coverage.
- `web/src/components/modules/group/health.tsx` — diagnostic terminology/action design only; it stops acting as the primary routing-health badge after PR B.
- `web/src/api/endpoints/group-health.ts` — frontend names/docs may use Diagnostic aliases while endpoint URLs remain compatible.
- `web/src/provider/locale.tsx` — merge feature messages.

---

### Task A1: Lock the routing/diagnostic dependency boundary

**Files:**
- Create: `internal/grouphealth/dependency_boundary_test.go`

**Invariant:** production Go files in `internal/grouphealth` may not import:

```text
github.com/bestruirui/octopus/internal/outlierwindow
github.com/bestruirui/octopus/internal/relay/balancer
github.com/bestruirui/octopus/internal/relay/availability
```

This is stronger than testing one failure path: it prevents a future probe implementation from silently starting to mutate the routing-health stores.

- [ ] **Step 1: Write the architectural test**

Use Go AST/package parsing over production `.go` files in the package, excluding `_test.go`, and fail with the offending file/import if any forbidden dependency appears.

- [ ] **Step 2: Run it against current main**

```bash
go test ./internal/grouphealth -run TestDiagnosticPackageDoesNotDependOnRoutingHealth
```

Expected: GREEN on current architecture. This is a characterization test, not a RED behavior change.

- [ ] **Step 3: Commit the invariant test**

```bash
git add internal/grouphealth/dependency_boundary_test.go
git commit -m "test(grouphealth): isolate diagnostics from routing health"
```

---

### Task A2: Replace the recognizable `ping` request with an ordinary minimal inference smoke test

**Files:**
- Modify: `internal/grouphealth/probe.go`
- Modify: `internal/grouphealth/probe_test.go`

**Decision:** use one neutral, deterministic request:

```text
What is 1+1?
```

with streaming disabled and a maximum output budget of **2 tokens** where the upstream protocol supports that field.

The answer body is **not graded**. A normal 2xx inference response is success; HTTP/protocol/connectivity failure is diagnostic failure. The goal is reachability, not correctness benchmarking.

This reduces obvious `ping`/`pong` probe signatures but does not claim to bypass providers that prohibit synthetic traffic. Because diagnostics remain manual and non-authoritative, a provider that rejects this request is simply “diagnostic failed/rejected”, not “routing unhealthy”.

- [ ] **Step 1: Add RED payload tests for every supported probe adapter**

Assert generated payloads:

- contain `What is 1+1?` instead of `ping`;
- are non-streaming where applicable;
- use output token budget 2;
- preserve the requested upstream model name;
- embedding input uses the same neutral ordinary text.

- [ ] **Step 2: Run focused RED**

```bash
go test ./internal/grouphealth -run 'Test.*Probe.*Payload'
```

Expected: fail because current payload is `ping` / one token.

- [ ] **Step 3: Implement the minimum payload change**

Do not add provider-specific text variants or randomized prompts in this slice.

- [ ] **Step 4: Run package tests**

```bash
go test ./internal/grouphealth
```

- [ ] **Step 5: Commit**

```bash
git add internal/grouphealth/probe.go internal/grouphealth/probe_test.go
git commit -m "fix(grouphealth): use neutral active diagnostic request"
```

---

### Task A3: Replace “Health / Full Probe” actions with one explicit Active Diagnostic control

**Files:**
- Modify: `web/src/components/modules/group/health.tsx`
- Modify: `web/src/api/endpoints/group-health.ts`
- Create: `web/src/provider/group-diagnostic-messages.ts`
- Modify: `web/src/provider/locale.tsx`
- Create/extend: `web/tests/group-diagnostic-semantics.test.mjs`

**UX decision:** one secondary action, **Active Diagnostic / 主动诊断**.

Opening it shows:

- explanation: “Sends a tiny real request. Diagnostic only; does not affect routing health.”;
- default traversal: **Follow failover path** — existing Standard semantics;
- optional checkbox/toggle: **Check all candidates / 检查全部候选** — existing Full semantics;
- last diagnostic timestamp/status as diagnostic history, not health score.

Do not expose “Standard” or “Full” as unexplained product nouns.

- [ ] **Step 1: Add RED UI contracts**

Assert visible copy uses `diagnostic`, `checkAllCandidates`, and a non-authoritative explanation, and that the component no longer labels the active snapshot simply as routing “health”.

- [ ] **Step 2: Keep API compatibility**

Existing backend probe mode values and URLs remain unchanged. Frontend may introduce semantic aliases:

```ts
export type GroupDiagnosticTraversal = 'route_path' | 'all_candidates';
```

mapped to existing `standard` / `full` request values at the endpoint adapter boundary.

- [ ] **Step 3: Add three-locale feature catalog**

Use the feature-catalog pattern already used elsewhere; no hard-coded visible English strings.

- [ ] **Step 4: Run frontend suite**

```bash
cd web
pnpm lint
pnpm test
pnpm build
```

- [ ] **Step 5: Commit**

```bash
git add web/src/components/modules/group/health.tsx web/src/api/endpoints/group-health.ts web/src/provider/group-diagnostic-messages.ts web/src/provider/locale.tsx web/tests/group-diagnostic-semantics.test.mjs
git commit -m "refactor(group): separate active diagnostics from health"
```

---

### Task A4: PR A verification gate

- [ ] Full repository CI green.
- [ ] Manual diagnostic success/failure still persists existing snapshot/attempt history.
- [ ] Failover route-path diagnostic stops after first success; “check all candidates” actually traverses every candidate.
- [ ] Non-Failover behavior remains compatible.
- [ ] Search changed production code and confirm no diagnostic call into outlierwindow/circuit/availability.
- [ ] No DB migration.

---

# PR B — Operational Health Inspection

## Architecture

Expose one read-only item inspection function from the balancer. It is the single source for the control-plane view and must call the same internal score/tier/circuit logic used for routing.

**Important:** this API observes runtime state; it must not call `IsTripped()` because that method performs circuit state transitions. Use the existing non-mutating `PeekItemTripped()`.

## File Structure

**Create**
- `internal/relay/balancer/operational_health.go`
- `internal/relay/balancer/operational_health_test.go`
- `internal/server/handlers/group_operational_health.go`
- `internal/server/handlers/group_operational_health_test.go`
- `web/src/api/endpoints/group-operational-health.ts`
- `web/src/provider/group-operational-health-messages.ts`
- `web/tests/group-operational-health.test.mjs`

**Modify**
- the existing Group API route registration file in `internal/server/handlers` to add the read-only endpoint alongside Group routes;
- `web/src/components/modules/group/index.tsx` — one page-level query/index;
- `web/src/components/modules/group/health.tsx` — Operational Health becomes the primary card badge/detail; Active Diagnostic becomes secondary.
- `web/src/provider/locale.tsx`.

---

### Task B1: Export an exact read-only balancer health snapshot

**Files:**
- Create: `internal/relay/balancer/operational_health.go`
- Create: `internal/relay/balancer/operational_health_test.go`

**Interfaces:**

```go
package balancer

import "time"

type OperationalHealthTier string

const (
    OperationalHealthTierGood     OperationalHealthTier = "good"
    OperationalHealthTierDegraded OperationalHealthTier = "degraded"
    OperationalHealthTierBad      OperationalHealthTier = "bad"
)

type ItemOperationalHealth struct {
    ChannelID            int                   `json:"channel_id"`
    ModelName            string                `json:"model_name"`
    Score                float64               `json:"score"`
    RoutingTier          OperationalHealthTier `json:"routing_tier"`
    Samples              int                   `json:"samples"`
    Failures             int                   `json:"failures"`
    FailureRate          float64               `json:"failure_rate"`
    ConsecutiveFailures  int                   `json:"consecutive_failures"`
    LastSuccessAt        time.Time             `json:"last_success_at,omitempty"`
    LastSampleAt         time.Time             `json:"last_sample_at,omitempty"`
    CircuitTripped       bool                  `json:"circuit_tripped"`
}

func InspectItemOperationalHealth(channelID int, modelName string, now time.Time) ItemOperationalHealth
```

Implementation rules:

1. call `outlierwindow.Evaluate(channelID, modelName, now)` exactly once;
2. derive `Score` through the existing health-score implementation — refactor `itemHealthScore` only enough to share the already-calculated `WindowStats`; do not copy its formula;
3. derive tier through the existing `healthTierOf` thresholds;
4. if `PeekItemTripped(...)` is true, effective `RoutingTier` is `bad`, exactly like HealthFirst ordering;
5. keep `Samples=0` explicit. A zero-sample item still has neutral routing score `0.5`, but the UI can label the evidence as “No data” rather than pretending a real degradation was observed.

- [ ] **Step 1: Write RED tests**

Cover:

- no samples -> score `0.5`, samples `0`, routing tier degraded;
- successful real reports improve score/tier;
- failures and consecutive failures lower the exact score according to existing logic;
- circuit tripped forces effective bad tier without calling mutating `IsTripped()`;
- inspecting does not mutate window/circuit state.

- [ ] **Step 2: Refactor score sharing minimally**

Recommended internal helper:

```go
func itemHealthScoreFromStats(st outlierwindow.WindowStats) float64
```

Then both `itemHealthScore()` and `InspectItemOperationalHealth()` use it.

- [ ] **Step 3: Run balancer suite**

```bash
go test ./internal/relay/balancer
```

- [ ] **Step 4: Commit**

```bash
git add internal/relay/balancer/operational_health.go internal/relay/balancer/operational_health_test.go internal/relay/balancer/balancer.go
git commit -m "feat(balancer): expose operational health snapshot"
```

---

### Task B2: Add a read-only Group Operational Health endpoint

**Files:**
- Create: `internal/server/handlers/group_operational_health.go`
- Create: `internal/server/handlers/group_operational_health_test.go`
- Modify: existing Group route registration in `internal/server/handlers`

**Endpoint:**

```text
GET /api/v1/group/operational-health/list
```

**DTO:**

```go
type GroupOperationalHealthItem struct {
    GroupItemID int                              `json:"group_item_id"`
    Priority    int                              `json:"priority"`
    Weight      int                              `json:"weight"`
    Health      balancer.ItemOperationalHealth   `json:"health"`
}

type GroupOperationalHealthView struct {
    GroupID       int                          `json:"group_id"`
    GroupName     string                       `json:"group_name"`
    GroupMode     model.GroupMode              `json:"group_mode"`
    Status        string                       `json:"status"`
    GoodCount     int                          `json:"good_count"`
    DegradedCount int                          `json:"degraded_count"`
    BadCount      int                          `json:"bad_count"`
    NoDataCount   int                          `json:"no_data_count"`
    Items         []GroupOperationalHealthItem `json:"items"`
}
```

### Display-only group status aggregation

This summary is **monitoring posture**, not a new routing algorithm and is never consumed by relay selection.

Apply in order:

1. no items -> `empty`;
2. every item has `Samples==0` and none is circuit-tripped -> `no_data`;
3. every item's effective routing tier is `bad` -> `unhealthy`;
4. at least one item is good -> `healthy`;
5. otherwise -> `degraded`.

Also return the tier/no-data counts so mixed groups are not hidden behind one badge.

Why this is intentionally not an “overall numeric health score”: routing modes consume item health differently. HealthFirst orders by tier/score, Failover preserves Priority before health, and other modes have their own ordering. One invented group number would imply semantics the relay does not have.

- [ ] **Step 1: RED handler/aggregation tests**

Cover empty, no-data, mixed good/bad, all-bad, and circuit-tripped groups.

- [ ] **Step 2: Implement read-only assembly**

Load groups through existing Group op code; inspect each `GroupItem` with `balancer.InspectItemOperationalHealth(...)` using one request-scoped `now := time.Now()`.

- [ ] **Step 3: Register GET route**

Keep the endpoint under authenticated Group control-plane routes. No write endpoint.

- [ ] **Step 4: Run Go tests**

```bash
go test ./internal/server/handlers ./internal/relay/balancer
```

- [ ] **Step 5: Commit**

```bash
git add internal/server/handlers/group_operational_health.go internal/server/handlers/group_operational_health_test.go <group-route-file>
git commit -m "feat(group): expose operational health view"
```

---

### Task B3: Make Operational Health the primary Group card signal

**Files:**
- Create: `web/src/api/endpoints/group-operational-health.ts`
- Create: `web/src/provider/group-operational-health-messages.ts`
- Modify: `web/src/components/modules/group/index.tsx`
- Modify: `web/src/components/modules/group/health.tsx`
- Modify: `web/src/provider/locale.tsx`
- Create: `web/tests/group-operational-health.test.mjs`

**Frontend query:** exactly one page-level observer:

```ts
export function useGroupOperationalHealthList() {
  return useQuery({
    queryKey: ['groups', 'operational-health'],
    queryFn: () => apiClient.get<GroupOperationalHealthView[]>(
      '/api/v1/group/operational-health/list',
    ),
    refetchInterval: 15000,
  });
}
```

The Group page memoizes a `group_id -> view` map and passes each view into the card. Never call this hook once per card.

### Card presentation

Primary badge/title: **Operational Health / 运行健康**.

Status semantics:

- `no_data`: neutral/unknown — “No recent real traffic”.
- `healthy`: at least one good real-traffic candidate; show counts in detail.
- `degraded`: no good candidate, but at least one non-bad/neutral candidate.
- `unhealthy`: every candidate has bad effective tier.
- `empty`: group has no candidates.

Expanded detail shows per-member:

- score;
- routing tier;
- sample count;
- failure rate;
- consecutive failures;
- last sample/success;
- circuit state.

Active Diagnostic is presented separately under a secondary action/history section with explicit text that it does not alter Operational Health.

- [ ] **Step 1: RED frontend contracts**

Assert:

- one page-level operational-health hook;
- card/health component does not instantiate the hook;
- no-data copy is distinct from degraded;
- diagnostic and operational-health copy are separate namespaces/sections.

- [ ] **Step 2: Implement endpoint types/hook**

Keep field names aligned exactly with Go DTO JSON.

- [ ] **Step 3: Integrate page-level index after the P0 shared-data pattern**

No N-card query observers.

- [ ] **Step 4: Add feature-scoped three-locale messages**

Minimum concepts: operationalHealth, realTrafficEvidence, noData, healthy, degraded, unhealthy, empty, samples, failureRate, consecutiveFailures, circuitOpen, diagnosticHistory, diagnosticDoesNotAffectRouting.

- [ ] **Step 5: Run frontend suite**

```bash
cd web
pnpm lint
pnpm test
pnpm build
```

- [ ] **Step 6: Commit**

```bash
git add web/src/api/endpoints/group-operational-health.ts web/src/provider/group-operational-health-messages.ts web/src/components/modules/group/index.tsx web/src/components/modules/group/health.tsx web/src/provider/locale.tsx web/tests/group-operational-health.test.mjs
git commit -m "feat(group): show passive operational health"
```

---

### Task B4: End-to-end semantic acceptance gate

- [ ] **Step 1: Full repository CI green**

Governance, Go Vet/full tests, frontend lint/tests/build.

- [ ] **Step 2: Prove real traffic changes Operational Health**

Use a remote test fixture with a known candidate. Send real relay successes/failures and confirm the Operational Health endpoint reflects the same rolling stats/score/tier used by routing.

- [ ] **Step 3: Prove diagnostics do not change Operational Health**

Capture Operational Health JSON, run both route-path and all-candidate Active Diagnostics, capture JSON again with no intervening real relay requests. Pass condition: samples/failures/score/circuit values are unchanged.

- [ ] **Step 4: Prove circuit display is read-only**

Inspect a tripped candidate repeatedly. Pass condition: inspection does not transition Open -> HalfOpen; only the normal request admission path may do that.

- [ ] **Step 5: Verify process-restart semantics**

After a controlled remote service restart, Operational Health correctly returns no-data/neutral in-memory state until real traffic rebuilds evidence. Existing persisted Active Diagnostic history may still exist and must remain visually separate.

- [ ] **Step 6: Verify provider that rejects diagnostics**

For a provider/channel that rejects the tiny synthetic request but still handles legitimate relay traffic, confirm:

- Active Diagnostic reports failure/rejection;
- Operational Health continues to reflect real request evidence;
- routing score/circuit is unchanged by the diagnostic failure.

- [ ] **Step 7: Scope gate**

Confirm no DB migration, no new diagnostic scheduler, no diagnostic-to-routing writes, and no modification to candidate selection semantics beyond the minimal score-helper refactor required to expose the exact existing value.
