# P1A Routing Inspector Design

> Status: **design approved for implementation planning; runtime implementation has not started**.
>
> Source-audit baseline: `main@a84de967172b86daa0b5fcc330654fee4dd43561`.
>
> Parent roadmap: [`2026-09-15-post-p0-gateway-operability-roadmap.md`](./2026-09-15-post-p0-gateway-operability-roadmap.md).

## Goal

Make ZyRealm routing decisions explainable to an authenticated operator without changing how routes are selected, retried, failed over, cooled down, recovered, or committed.

P1A.1 answers two questions for a **real historical request**:

1. Why was the final provider / credential / protocol selected?
2. Why were other candidates or plans not used?

The implementation must reuse the existing routing and relay-log model. It must not create a second scheduler, a second live-request registry, or a parallel durable trace table.

## Source-audit findings

The current runtime already persists most post-selection routing facts:

- `internal/model/attempt_routing_trace.go` embeds structured failure domain/scope, rule, retry directive, runtime effect/state, cooldown, replay safety, dispatch state, commitment state, and provider/wire attempt indexes into each `ChannelAttempt`.
- `internal/model/log.go` persists `RelayLog.Attempts` through `gorm:"serializer:json"`; extending the nested JSON payload does not require a new database column.
- `internal/relay/balancer/iterator.go` already treats skips, circuit breaks, capacity/rate rejection, and real attempts as one ordered decision stream.
- `internal/server/handlers/log.go` already exposes authenticated log detail through `GET /api/v1/log/:id`.
- `web/src/components/modules/log/Item.tsx` already has a retry/diagnostic surface where routing explanation belongs.

The audit also identified information that is currently discarded before serialization:

1. `internal/relay/balancer/runtime_candidates.go` silently removes provider/provider-model candidates that are in runtime cooldown.
2. `internal/relay/credential_fair.go` silently removes credentials rejected by credential-local runtime availability before fair selection.
3. `internal/relay/capability_cache.go` can remove one protocol plan because of learned capability-negative evidence while another plan survives; the blocked plan is currently lost unless all plans are blocked.

A second important constraint is attempt cardinality. `RelayMetrics.saveLog` currently stores `TotalAttempts = len(attempts)`, and the existing frontend uses attempt count to decide whether to render retry UI. Therefore P1A must **not** append one new top-level `ChannelAttempt` for every pre-dispatch rejection. Doing so would make an ordinary single-dispatch request look like a multi-attempt retry.

## Design decision

Use a **typed nested routing-decision ledger inside the existing `ChannelAttempt` JSON envelope**.

```text
real request
   |
   +--> runtime candidate filtering
   |      +-- decision events
   |
   +--> credential filtering / fair selection
   |      +-- decision events
   |
   +--> protocol plan filtering
   |      +-- decision events
   |
   +--> existing Iterator / ChannelAttempt execution trace
          +-- existing routing trace fields
          +-- nested decision_events[]
                    |
                    v
              RelayLog.Attempts JSON
                    |
                    v
          pure RoutingExplanation builder
                    |
                    v
        GET /api/v1/log/:id/routing
                    |
                    v
             Routing Inspector UI
```

This is an observability extension to the existing decision stream, not a new scheduling abstraction.

## Hard invariants

### 1. Historical truth only

A historical explanation must be derived only from facts captured during the original request and persisted in that request's `RelayLog`.

The explanation builder must not re-read current provider health, current credential cooldown, current capability cache, current configuration, or current fairness state to infer what happened in the past.

### 2. Observationally neutral capture

Recording an explanation event must not:

- alter provider ordering;
- alter credential fairness allocation;
- acquire or release half-open leases;
- change concurrency/RPM accounting;
- create, extend, shorten, or clear cooldowns;
- change EWMA/outlier state;
- create or clear capability-negative observations;
- change retry budgets or replay safety;
- mutate WebSocket affinity/replay state;
- perform an upstream request.

### 3. Preserve existing attempt cardinality

For every request that already produces one or more `ChannelAttempt` records, adding P1A decision events must preserve:

- `len(attempts)`;
- existing `AttemptNum` values;
- attempt ordering;
- success/failed/skipped statuses;
- site-action target indexes;
- final-channel/final-model resolution.

The nested ledger is attached to an existing persisted attempt envelope rather than emitted as extra top-level attempts.

### 4. Zero-attempt exception is explicit

If a request has routing decision events but **no existing attempt envelope at all** because every candidate was eliminated before the current Iterator can emit a record, P1A may persist one `AttemptSkipped` envelope with `attempt_kind = "decision_only"` solely to carry the ledger.

That exception must satisfy all of the following:

- it cannot be interpreted as a wire dispatch;
- it cannot increment provider/wire attempt budgets;
- it cannot be counted as success/failed channel traffic;
- it cannot acquire runtime leases or capacity;
- existing final-channel/final-model logic must continue to ignore it;
- frontend retry UI must not label one `decision_only` envelope as a retry.

Current site-model attempt accounting already ignores statuses other than success/failed; tests must lock that property down for `decision_only`.

### 5. No sensitive payloads

Decision events may contain stable IDs and bounded operational metadata, but must not contain:

- credential/API-key secret values;
- authorization headers;
- raw request or response bodies;
- prompt text;
- tool arguments/results;
- arbitrary upstream HTML/error bodies;
- full custom headers.

Human-readable detail must be bounded and sanitized. Machine interpretation must use enum-like reason codes rather than parsing message text.

### 6. Backward compatibility

Old logs without decision events must remain readable. The explanation API must return a useful **partial/legacy** explanation from existing `ChannelAttempt` and `AttemptRoutingTrace` fields rather than failing.

## Data contract

Add a small model contract, preferably in `internal/model/routing_decision_event.go`.

Conceptually:

```go
type RoutingDecisionStage string

type RoutingDecisionOutcome string

type RoutingDecisionReason string

type RoutingDecisionEvent struct {
    Sequence      int                    `json:"sequence"`
    Stage         RoutingDecisionStage   `json:"stage"`
    Outcome       RoutingDecisionOutcome `json:"outcome"`
    Reason        RoutingDecisionReason  `json:"reason,omitempty"`
    ChannelID     int                    `json:"channel_id,omitempty"`
    ChannelKeyID  int                    `json:"channel_key_id,omitempty"`
    ModelName     string                 `json:"model_name,omitempty"`
    Protocol      string                 `json:"protocol,omitempty"`
    RuntimeState  string                 `json:"runtime_state,omitempty"`
    CooldownUntil int64                  `json:"cooldown_until,omitempty"`
    Detail        string                 `json:"detail,omitempty"`
}
```

Exact field names may be adjusted during implementation, but the contract must stay bounded and typed.

Initial stages:

- `candidate`
- `credential`
- `protocol`
- `dispatch`
- `failover`

Initial outcomes:

- `eligible`
- `rejected`
- `selected`
- `attempted`
- `stopped`

Initial reason vocabulary should cover existing behavior rather than anticipate hypothetical future features. Candidate reasons include:

- `runtime_cooldown`
- `runtime_suspect`
- `credential_cooldown`
- `capability_negative`
- `circuit_break`
- `capacity`
- `rate_limit`
- `protocol_incompatible`
- `attempt_budget`
- `replay_unsafe`

Existing structured routing fields remain authoritative for failure-domain classification, retry directives, runtime effects, replay safety, and failover stop reason. Do not duplicate those fields into every decision event merely to make the event self-contained.

Add one optional nested field to the existing serialized attempt trace, for example:

```go
DecisionEvents []RoutingDecisionEvent `json:"decision_events,omitempty"`
```

Because `RelayLog.Attempts` is already JSON-serialized, this remains a no-migration change.

## Event collection and anchoring

The preferred implementation boundary is the existing `balancer.Iterator`, because it already owns the ordered attempt/skip decision stream.

Add a side-effect-free collector such as `RecordDecision(event)` that only appends bounded metadata to an in-memory request-local ledger.

`Attempts()` should continue to return the same top-level decision/attempt sequence. When materializing the persisted snapshot it should:

1. copy the attempt slice rather than mutate scheduler state;
2. attach the ledger once to a deterministic existing envelope when at least one attempt exists;
3. synthesize one `decision_only` skipped envelope only when no attempt exists but the ledger is non-empty;
4. be idempotent if called repeatedly.

Attaching the ledger during snapshot/materialization keeps routing selection independent from persistence formatting.

## P1A.1 capture scope

### Provider / provider-model runtime filtering

When `runtimeOrderedCandidates` observes a candidate in cooldown and removes it from executable candidates, capture a `candidate / rejected / runtime_cooldown` event using the state snapshot already available at that decision point.

If suspect candidates are placed in the fallback tier, a bounded `candidate / eligible / runtime_suspect` event may be captured if it materially improves explanation. It must not change ordering.

Do not call `AcquireCandidate` merely to explain a candidate. Half-open lease semantics remain exactly where they are today.

### Credential filtering

When `selectFairChannelCredential` rejects a key through `CredentialAvailableRevision`, capture `credential / rejected / credential_cooldown` with channel/key IDs and only the runtime metadata already safely available at selection time.

The surviving key set passed into `SelectCredentialFair` must be byte-for-byte/ID-for-ID equivalent to the current behavior.

Circuit-break skips already have top-level structured representation. The explanation builder can project those existing records instead of emitting a duplicate event.

### Capability-negative protocol filtering

Refactor `filterCapabilityNegativePlans` so it can return the surviving plans plus bounded blocked-plan snapshots.

For each blocked plan, capture `protocol / rejected / capability_negative`, including upstream model/protocol and expiry if already available in the capability snapshot.

Critical regression case:

```text
responses plan        -> blocked by learned capability negative
chat-completions plan -> survives
```

The surviving plan and final dispatch must remain identical to current behavior while the blocked `responses` plan becomes explainable.

### Existing execution/failure trace

Do not duplicate existing information. The explanation builder should reuse:

- protocol mode / selected protocol / fallback reason;
- failure domain / scope / rule;
- retry directive;
- runtime effect/state/cooldown;
- replay safety;
- dispatch state;
- downstream committed state;
- provider/wire attempt indexes;
- failover stop reason.

## Routing explanation projection

Create a pure projection layer in `internal/relay/routing_explanation.go`.

It receives a persisted `model.RelayLog` (or only the immutable fields required from it) and returns a stable operator DTO. It must not read process-global runtime state.

Suggested top-level shape:

```text
RoutingExplanation
  version
  completeness: complete | legacy_partial | decision_only
  requested_model
  final_route
  steps[]
  summary
```

Each `step` should be derived from persisted decision events or existing attempt trace fields and carry a stable machine reason plus concise display fields.

For old logs with no P1A decision ledger, return `legacy_partial` and reconstruct only what is provable from the old attempt trace. Never invent omitted candidates.

## API

Add an authenticated endpoint in the existing log route group:

```text
GET /api/v1/log/:id/routing
```

Handler behavior:

1. parse and validate log ID;
2. load the persisted log with the existing `op.RelayLogGet` path;
3. run the pure explanation projection;
4. return the stable DTO;
5. use existing not-found/error conventions.

Do not expose a separate persistence layer or recompute routing from current configuration.

## Frontend

Extend `web/src/api/endpoints/log.ts` with the explanation DTO and a detail fetcher.

Add a focused component, preferably:

```text
web/src/components/modules/log/RoutingInspector.tsx
```

Wire it into the existing Log Detail diagnostic surface in `Item.tsx`.

The default view should be compact:

```text
Requested model
  -> 4 candidates
  -> 2 rejected
  -> provider B / credential selected
  -> Responses rejected: learned capability
  -> Chat Completions attempted
  -> provider-model failure
  -> reroute safe
  -> provider C success
```

Advanced fields such as rule ID, failure scope, cooldown expiry, dispatch state, or replay safety can be expandable. Do not dump the raw serialized trace by default.

The current retry badge and existing attempt list keep their present attempt-count semantics; nested decision events must not create fake retry badges.

## Live Requests boundary

P1A.1 does **not** add full candidate history to `LiveRequestSnapshot`.

Active Requests may continue to show current channel/key/protocol/dispatch/commit state. Adding complete live candidate-history streaming would duplicate trace state in the P0.3 live registry and is deferred until there is evidence it is needed.

## Dry-run / simulator boundary

A route simulator is explicitly deferred to P1A.2.

Current production selection contains stateful operations, including round-robin/health-first rotation, credential fairness allocation, half-open lease acquisition, concurrency admission, and RPM admission. Calling the production selector for a diagnostic dry-run cannot yet be proven observationally neutral.

A later simulator should first separate a pure planner from side-effect application, conceptually:

```text
immutable runtime snapshot -> pure planner -> simulated route
real execution             -> planner + explicit side-effect commit
```

P1A.1 must not build a second scheduler to approximate this behavior.

## Security and boundedness

- Keep event count bounded by actual configured candidates/credentials/protocol plans encountered during one request.
- Bound free-form `detail` text; prefer reason enums.
- Never serialize credential secret values.
- Reuse existing authenticated log access.
- Do not include raw request/response content in the explanation DTO.
- Preserve existing content-inclusion controls on normal log endpoints.

## Acceptance criteria

P1A.1 is complete only when all of the following are proven by tests and CI:

1. A normal one-dispatch success still has exactly the same top-level attempt count and attempt numbers as before P1A.
2. Runtime-cooldown candidate rejection is visible in the persisted routing ledger without altering candidate order.
3. Credential-cooldown rejection is visible without changing the surviving credential set or fairness choice.
4. A capability-negative protocol plan is visible even when another protocol plan survives and succeeds.
5. Existing circuit/capacity/rate/failure/replay/commit fields are projected without duplicate semantic records.
6. Old logs without decision events return a valid `legacy_partial` explanation.
7. A zero-attempt all-filtered request can be explained through the explicit `decision_only` envelope without being counted as success/failed channel traffic or a wire dispatch.
8. `GET /api/v1/log/:id/routing` is authenticated and returns no secret/prompt/raw payload material.
9. Routing Inspector renders the compact explanation while preserving current retry UI semantics.
10. Governance, backend full tests, frontend lint/test/build all pass in GitHub CI; no local Mac build/test is used.

## Non-goals

- No DB migration.
- No scheduler algorithm change.
- No provider/credential weight change.
- No retry/failover policy change.
- No cooldown/half-open behavior change.
- No synthetic health probe.
- No route simulator in P1A.1.
- No full live decision-history registry.
- No raw prompt/response forensic storage.
- No production deployment as part of this planning/design slice.
