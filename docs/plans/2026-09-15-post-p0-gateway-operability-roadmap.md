# Post-P0 Gateway Operability Adoption Roadmap

> Status: **proposal for review only**. No runtime, schema, API, frontend, deployment, or production-state change is approved by this document.
>
> Planning baseline: ZyRealm `main@a84de967172b86daa0b5fcc330654fee4dd43561` (2026-09-15). The latest runtime-changing baseline remains P0.3 squash merge `c60ba21cebe5d3b9424d2fffff97964e2b281deb`, whose merged-tree CI run `34960012680` passed governance, backend Vet/full tests, and frontend lint/test/build.
>
> Related roadmap: [`2026-09-14-octopus-gap-roadmap.md`](./2026-09-14-octopus-gap-roadmap.md).

## Goal

Turn ZyRealm's already-advanced adaptive routing core into an **explainable, diagnosable, and operable gateway** without replacing its existing failure-domain, replay-safety, credential fairness, cooldown, passive half-open, protocol fallback, or capability-learning semantics.

This roadmap was informed by a comparison with `My-Search/my-ai-gateway`, but useful product/control-plane ideas are transferred only where they fit ZyRealm's current runtime architecture. Its simpler failover loop, synthetic probing behavior, and generic provider-switching semantics are not adoption targets.

## Source-audit corrections before planning implementation

A first-pass audit of current ZyRealm `main` changes the initial feature framing in several important ways:

1. **Capability negative learning already exists.** `internal/relay/capability_cache.go` fingerprints request shape, protocol plan, model, and capability-affecting channel configuration. `internal/relay/availability/capability.go` stores exact negative observations process-locally with a fixed **30-minute TTL** and a **4096-entry bound**. Successful real traffic can clear exact stale negative evidence early.
2. **Passive half-open already exists.** `internal/relay/availability/runtime.go` models provider/provider-model states as available, suspect, cooldown, and half-open. `AcquireCandidate` grants a single real-traffic recovery lease after cooldown expiry. Do not add a synthetic provider probe loop unless later production evidence proves passive recovery inadequate.
3. **Channel mutation already clears routing memory.** `internal/relay/availability/reset_channel.go` clears provider/model cooldown state, credential runtime state, fairness history, and capability-negative observations for the modified channel.
4. **P0.3 already provides live request control.** `internal/relay/live_request.go` and the authenticated Active Requests surface are the current runtime-control foundation. New diagnostics should compose with that layer rather than create a second live-request registry.
5. **Routing already has structured decision primitives.** `internal/relay/routing_decision.go`, `route_learning.go`, `failure_domain.go`, `failure_scope.go`, and `failover_stop_reason.go` are candidate foundations for explanation output. P1A must audit and reuse them before inventing new trace types.

Therefore the post-P0 sequence should prioritize **exposing and explaining existing state before adding new routing state**.

---

## P1A — Explain Routing / Route Diagnostics — NEXT SOURCE AUDIT

### Why first

ZyRealm now makes routing decisions from multiple interacting signals: configured provider order, credential selection/fairness, provider/model availability, learned capability exclusions, protocol attempt planning, replay safety, downstream commitment, and failover budgets. Operators need to answer "why this route?" and "why not the others?" without reading raw logs or source code.

### Intended operator contract

For one real request or an explicit dry diagnostic input, expose a bounded explanation such as:

```text
requested model
  -> eligible group items
  -> provider / credential candidates
       - rejected: capability negative evidence
       - rejected: credential cooldown
       - deferred: provider-model cooldown
       - eligible: candidate rank / fairness inputs
  -> protocol attempt plan
  -> selected candidate
  -> dispatch / commitment / failover result
```

The explanation should distinguish at least:

- static/configuration exclusion;
- provider/model runtime availability exclusion;
- credential runtime exclusion;
- learned capability-negative exclusion, including expiry when present;
- protocol incompatibility/fallback choice;
- attempt-budget or replay-safety stop reason;
- final selected route.

### Critical design constraint

A diagnostic/dry-run path must be **observationally neutral**. It must not:

- increment active-request or fairness accounting as if real traffic executed;
- update EWMA/latency/outlier health;
- increment failure streaks or create cooldowns;
- create or clear capability-negative evidence;
- consume half-open recovery leases;
- mutate WebSocket affinity/replay state;
- write durable request payloads.

If a safe dry-run cannot reuse the production selector without side effects, P1A should initially expose explanations from **real completed/live routing traces only** rather than introduce a second scheduler implementation.

### Fresh source-audit surface before implementation

At minimum deep-read:

- `internal/relay/routing_decision.go`
- `internal/relay/route_learning.go`
- `internal/relay/relay_handler.go`
- `internal/relay/relay_attempt.go`
- `internal/relay/protocol_attempt.go`
- `internal/relay/capability_cache.go`
- `internal/relay/availability/*`
- `internal/relay/live_request.go`
- existing relay log serialization and Logs/Active Requests frontend paths

### P1A gate

Do not write implementation code until the audit answers:

1. Which current decision/attempt fields already contain enough information for explanation?
2. Which rejection reasons are currently discarded before serialization?
3. Can explanation be generated from existing immutable snapshots, or is one bounded new trace object required?
4. Can a dry-run be proven state-neutral, or should v1 be real-traffic explanation only?
5. What data is safe for an authenticated admin surface without exposing credential values or raw prompts?

**Preferred v1:** explanation of real routing decisions using existing trace/runtime primitives, with no DB migration.

---

## P1B — Capability Knowledge Layer — AFTER P1A

### Revised scope

Do **not** build a new credential × model × protocol relational schema as the first move. Runtime capability learning already exists and is request-shape aware. The missing piece is optional **declarative positive/negative capability hints** that can eliminate obviously impossible candidates before a real failed attempt, while remaining subordinate to runtime truth.

Conceptually:

```text
Capability Knowledge
  = declared hints/configuration
  + learned exact negative evidence
  + protocol/request feature requirements
        -> candidate eligibility explanation
```

### Candidate semantics to audit

Potential declarative dimensions include text, image input, tools/tool choice, reasoning/thinking, audio, structured response formats, and protocol-specific constraints. These must map onto the request-feature model already used by `protocolroute.RequestFeatureFlags` and the existing capability signature; do not introduce a parallel feature vocabulary unless the audit proves necessary.

### Required precedence rules

The implementation plan must explicitly define cases such as:

- declared unsupported => reject before dispatch;
- declared supported + learned exact negative => learned negative temporarily wins for that exact shape;
- unknown => allow normal routing/learning;
- channel configuration change => stale learned evidence remains version-isolated by existing config fingerprinting;
- successful real traffic => may clear matching learned negative evidence;
- declarations must never convert request-scoped capability incompatibility into provider-health failure.

### P1B gate

Before any schema or persistent field is approved, audit whether existing channel configuration JSON/settings can carry bounded hints safely. A DB migration requires evidence that configuration-backed hints are insufficient.

**Preferred v1:** small declarative hints composed with existing runtime capability cache; no schema explosion.

---

## P1C — Routing Health / Failure-Domain Operations Console — AFTER P1A

### Goal

Expose existing process-local availability state to authenticated operators and add narrowly scoped manual recovery actions where safe.

The control plane should make provider, provider-model, credential, and capability-negative state inspectable without reimplementing the scheduler. Candidate information includes:

- state: available / suspect / cooldown / half-open;
- bounded reason;
- cooldown expiry;
- credential cooldown/eligibility status;
- capability-negative reason and expiry;
- active half-open lease when safely representable;
- recent route/failover outcome summary where already available.

### Manual recovery boundary

A recovery action may clear process-local routing memory for an explicitly selected scope, but it must not:

- mark an unhealthy provider healthy through synthetic success;
- fabricate successful observations;
- change retry/replay rules;
- bypass authorization/model policy;
- trigger synthetic billable model requests;
- silently clear unrelated channels/scopes.

Reuse or factor the semantics already present in `availability.ResetChannel` rather than creating divergent reset behavior.

### Ordering relative to P1A

P1A comes first because an operator should be able to understand *why* a state exists before being given controls to clear it. P1C may share read-only snapshot primitives created by P1A, but mutation APIs must remain a separate review slice.

---

## P2A — Optional Raw Payload Forensics — DEFERRED

Keep three concerns separated:

```text
LiveRequest   -> transient execution/control state
RelayLog      -> durable normalized metadata
RawPayload    -> optional sensitive forensic material with bounded retention
```

Any future raw-payload feature must be explicit opt-in, authenticated, retention-bounded, and designed so normal logging continues to work with raw storage disabled. Do not add prompt/response bodies to P0.3 live snapshots or ordinary routing-health state.

This slice requires a separate privacy/security review before implementation.

---

## P2B — Request Transformation Policy — DEFERRED

`my-ai-gateway` prompt injection is useful as a product idea but should not be copied as protocol-specific preprocessing. If ZyRealm later needs system-prompt injection, tool policy, reasoning policy, or token-budget transforms, implement them through one canonical request-policy boundary before protocol encoding rather than scattered `if OpenAI / if Anthropic / if Gemini` branches.

Before planning P2B, audit the current transformer/protocolroute boundaries and confirm there is an actual user requirement. No prompt-injection feature is approved by this roadmap.

---

## Explicit non-goals

- No replacement of ZyRealm's existing adaptive routing loop with `my-ai-gateway` failover behavior.
- No generic "any error => switch provider" policy.
- No synthetic health probe loop in P1.
- No duplicate capability-negative cache.
- No second live-request registry.
- No P1 database migration by default.
- No permanent raw prompt/response logging by default.
- No protocol-specific prompt injection scattered through relay handlers.
- No change to replay-safety, downstream-commitment, credential fairness, timeout, retry budget, cooldown, or passive half-open semantics unless a later source audit identifies a concrete defect.
- No deployment or production-state mutation as part of planning/docs PRs.

## Proposed execution order

```text
P0.1 / P0.1H / P0.2 / P0.3  MERGED + VERIFIED
                    |
                    v
P1A Explain Routing / Route Diagnostics
   source audit -> design -> TDD implementation -> review -> merge/verify
                    |
                    +--------------------+
                    |                    |
                    v                    v
P1B Capability Knowledge         P1C Routing Health Console
(declared + learned)             (inspect first, mutate second)
                    \                    /
                     \                  /
                      -> operational feedback
                              |
                              v
                P2A Raw Forensics / P2B Transform Policy
                         only if still needed
```

P1B and P1C are logically separable after P1A. They should be implemented as independent PRs and may be reordered based on operational evidence.

## Approval / progress checklist

- [x] P0.1, P0.1H, P0.2, and P0.3 merged and verified.
- [x] Initial `my-ai-gateway` comparison completed.
- [x] Post-P0 first-pass source audit confirmed existing capability-negative cache and passive half-open semantics.
- [x] Post-P0 candidate sequence reduced to explainability/operability rather than another routing-algorithm rewrite.
- [ ] User approves or edits this roadmap ordering.
- [ ] P1A fresh source audit completed against then-current `main`.
- [ ] P1A detailed design/implementation plan written from source-audit evidence.
- [ ] P1A implemented, reviewed, merged, and merged-tree verified before P1B/P1C implementation begins.
- [ ] P1B need re-evaluated after P1A operational feedback.
- [ ] P1C need/scope re-evaluated after P1A operational feedback.
- [ ] P2A/P2B remain deferred until separately approved.
