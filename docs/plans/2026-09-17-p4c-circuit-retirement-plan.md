# P4C Circuit Retirement Plan

## Goal

Make `internal/relay/availability` the sole immediate routing-health authority without silently changing replay, credential, route-learning, outlier, Compact, WebSocket, or managed-site behavior.

P4C is intentionally staged. Do **not** delete `internal/relay/balancer/circuit.go` until every live reader has been migrated to an equivalent availability path and circuit writes are proven unnecessary.

Baseline: `main@7fc188c1b08a347179d3a2efda1f84a148b29c3d`.

## Fresh audit findings

1. Core runtime candidate admission is already owned by availability.
2. Core credential selection still had a revision-1 compatibility read through `selectFairChannelCredential -> Iterator.SkipCircuitBreak -> IsTripped`.
3. Shared `Failover.Candidates` and `HealthFirst.Candidates` still used `PeekItemTripped` as an extra ordering authority.
4. Images was migrated in P4B to runtime/credential availability admission, but compatibility circuit writes remain.
5. Core relay still writes legacy circuit success/failure evidence.
6. Managed route learning is coupled to breaker semantics: `maybeLearnManagedRoute` runs only when `circuitFailureKindForDecision(...) == FailureHard`.
7. Credential replacement is already revision-safe: `ChannelKey.BeforeUpdate` increments `credential_revision` when the secret changes. Revision 1 is only the initial identity.
8. Generic/low-confidence HTTP 5xx is runtime-neutral and contributes passive outlier evidence. P4C must not promote it into availability cooldown merely to mimic the old breaker.
9. An exact-head compile gate after deleting `Iterator.SkipCircuitBreak` exposed three missed live readers:
   - `internal/relay/compact.go`
   - WebSocket warmup in `internal/relay/ws_client.go`
   - WebSocket relay in `internal/relay/ws_client.go`
10. Deeper audit showed Compact/WS are not yet symmetric with core/Images for availability evidence:
    - WS attempts already produce a `RoutingDecision`, but `runWSRelay` does not apply runtime/credential availability effects like the core handler does.
    - Compact still uses its own outlier + compatibility-breaker path and does not yet have the same availability-effect bridge.

Finding 10 changes the migration boundary. Removing the Compact/WS reader before migrating their health writers could weaken future bad-key/bad-model isolation. Therefore P4C1 is split into P4C1a and P4C1b.

## Target invariant after all P4C slices

One wire outcome -> one `RoutingDecision` -> independent effects:

- current-request retry/failover/replay safety;
- runtime availability (`Available/Suspect/Cooldown/HalfOpen`);
- credential availability;
- passive `outlierwindow` statistics;
- capability/feature suppression;
- managed-route learning, explicitly independent from health admission.

---

## P4C1a — Remove legacy circuit authority from core/shared routing

Status: ✅ merged as PR #45 (`0d5b4e2c38e387de8782d70967adcaf19b992717`).

### Scope

Core/shared only:

- all core credential revisions use `CredentialAvailableRevision` for admission;
- revision-1 no longer has a hidden circuit gate in `selectFairChannelCredential`;
- shared Failover ordering is priority + passive model health only;
- shared HealthFirst tiers are passive model health only;
- remove the `includeLegacyCircuit` dual ordering path;
- keep Compact/WS `Iterator.SkipCircuitBreak` compatibility reader intact until P4C1b;
- keep all circuit writers, `CircuitEffect`, route-learning coupling, circuit settings, and `circuit.go` intact.

### RED contracts

- a healthy revision-1 core credential remains selectable even when a stale circuit entry is open;
- shared Failover order is unchanged by a legacy circuit entry;
- shared HealthFirst tiers are unchanged by a legacy circuit entry;
- runtime availability cooldown remains authoritative independently of legacy circuit state.

RED evidence:

- CI `35236762792`: exactly the three initial P4C1 authority contracts failed; governance/frontend/Vet were green.
- after replacing two old strategy circuit-ordering contracts with the new desired behavior, CI `35237304145` remained red for the expected legacy-reader semantics only.

### Compatibility tests retained by rewriting, not deleting

Two live relay tests mixed useful behavior with obsolete core breaker admission semantics:

- multi-key fallback now seeds authoritative credential cooldown and still proves the next healthy key is used;
- generic HTTP 500 may still create a compatibility circuit entry, but that entry must not block the next **core** relay admission;
- the existing 429 test continues to guard provider-model runtime cooldown.

Behavior GREEN before attempting dead-reader deletion: CI `35238965465` — governance, backend Vet/full Test, frontend lint/Test/Build all green.

### Caution gate that changed the plan

Deleting `Iterator.SkipCircuitBreak` produced exact-head CI `35239301771`, which failed compilation at Compact and two WS callsites. This was treated as a source-audit failure, not fixed by restoring global breaker authority or by forcing the sidepaths through an incomplete migration.

A short-lived experiment replaced those sidepath readers with an ordered availability helper while preserving `Channel.GetChannelKey` preferred/lowest-cost behavior. Diff audit was clean, but semantic review showed Compact/WS did not yet produce equivalent availability evidence. The experiment was removed from the branch before the P4C1a Draft PR.

### P4C1a stop gate

- core credential admission does not call circuit state for any credential revision;
- shared Failover/HealthFirst ordering does not read circuit state;
- core generic-500 compatibility breaker writes are inert for core admission;
- Compact/WS legacy reader remains explicitly documented and unchanged;
- circuit writers remain reversible compatibility writes;
- route-learning behavior is unchanged;
- full exact-head GitHub Actions CI green;
- no deployment.

Deliverable: Draft PR only. Do not merge automatically.

---

## P4C1b — Migrate Compact and WebSocket before removing the final reader

Status: ✅ merged as PR #46 (`080aa59d9888ca4b7377515caa272608d21e2bc7`).

### Required behavior before reader removal

Compact and WS must first write authoritative availability evidence, not merely consume it.

For WS:

- reuse the `RoutingDecision` already attached by `relayAttempt.attempt()`;
- apply `recordRuntimeAvailabilityEvidence` at the WS orchestration boundary;
- apply revision-aware credential failure/success effects with the same policy source as core;
- preserve committed-stream no-replay behavior and WS conversation-reset semantics.

For Compact:

- add a thin Compact-to-`attemptResult`/`RoutingDecision` bridge rather than a new classifier;
- apply runtime and credential availability effects from that decision;
- preserve Compact request/response passthrough, upstream-model keying, sticky semantics, retry behavior, and outlier accounting.

Only after those effects are covered by RED/GREEN tests:

- preserve `Channel.GetChannelKey` ordering (preferred key, then lowest `TotalCost`);
- replace circuit admission with revision-aware credential availability while keeping that ordering;
- remove the two WS and one Compact `SkipCircuitBreak` callsites;
- then delete `Iterator.SkipCircuitBreak` and its obsolete breaker-only state.

### P4C1b stop gate

- no live scheduling/credential admission path reads `IsTripped`, `PeekItemTripped`, or `SkipCircuitBreak`;
- Compact/WS high-confidence failures still create the correct future availability state;
- no fairness/sticky/retry/replay semantics changed incidentally;
- full exact-head CI green.

---

## P4C2 — Decouple route learning, then remove circuit writes and policy surface

P4C2 is split into two independently verified slices. P4C2A is merged; P4C2B is the current Draft PR boundary.

### P4C2A status

✅ merged as PR #47 (`31562a22dd539ee6bbd2f675cc22f8a37a1ce366`). Route learning now uses an explicit `shouldLearnManagedRoute` policy and no longer depends on `balancer.FailureKind` / `FailureHard`.

### P4C2B status

🚧 Draft PR #48. Live Core / Images / Compact / WebSocket circuit writes are removed; `circuit.go`, reset/settings compatibility, and `CircuitEffect` policy/trace compatibility remain for P4C3.

### Route-learning decoupling

- add an explicit route-learning eligibility policy based on the already-computed `RoutingDecision`, retry policy, and status;
- preserve current hard/soft/ignored learning behavior;
- remove route-learning dependence on `balancer.FailureKind` / `circuitFailureKindForDecision`;
- do not add another health state machine or re-parse raw errors downstream.

### Remove writers

After RED contracts cover core HTTP, Images, Compact, WS, committed stream failures, credential failures, model capacity, provider transients, content policy, success, and route learning:

- remove live `balancer.RecordSuccess` / `RecordFailure` calls;
- remove `circuitFailureKind` / `circuitFailureKindForDecision` after route learning is independent;
- retain `RoutingDecision.CircuitEffect` through P4C2B because route-learning compatibility and attempt tracing still consume the token; review and remove/rename that surface in P4C3.

P4C2 stop gate:

- zero live circuit readers;
- zero live circuit writers;
- route learning independently tested;
- exact-head source audit confirms no non-test `internal/relay` caller of `balancer.RecordFailure`, `balancer.RecordSuccess`, or `circuitFailureKind*`;
- `circuit.go` retained only as dead compatibility code pending final cleanup review;
- `CircuitEffect` retained temporarily as a compatibility policy/trace token, not as a breaker writer;
- full CI green.

P4C2B RED evidence:

- initial RED exposed a test-fixture ordering issue for two Core cases; the fixture was corrected without production changes;
- clean RED run `35299692585` then failed exactly the seven writer-retirement contracts across Core / Images / Compact / WebSocket;
- first GREEN run `35300007859` passed governance, backend Vet/full Go tests, and frontend lint/test/build.

---

## P4C3 — Delete dead breaker implementation and compatibility settings

Start only after a fresh repository-wide audit confirms zero live references.

Candidate cleanup:

- delete `internal/relay/balancer/circuit.go` and circuit-only tests;
- remove circuit reset plumbing;
- remove dead circuit settings and matching UI/i18n only after backward-compatibility review;
- remove obsolete circuit-specific trace/comments/tests;
- keep passive outlier/POR policy unchanged;
- keep availability cooldown policy unchanged unless separately justified by production evidence.

Final gate:

- no circuit implementation, reader, writer, or active configuration surface remains;
- revision-1 and later credentials share the same availability authority everywhere;
- full CI green on the exact final tree;
- no DB migration;
- no deployment.

## Explicit non-goals

- no retry-budget reduction;
- no AttemptCoordinator/P5 work;
- no cancellation-source/P6 work;
- no module-path rename;
- no POR redesign;
- no Images fairness redesign;
- no deployment.
