# P4C Circuit Retirement Plan

## Goal

Make `internal/relay/availability` the sole immediate routing-health authority without silently changing replay, credential, route-learning, outlier, or managed-site behavior.

P4C is intentionally staged. Do **not** delete `internal/relay/balancer/circuit.go` until production circuit readers are gone and circuit writes are proven inert.

Current baseline: `main@7fc188c1b08a347179d3a2efda1f84a148b29c3d`.

## Fresh source-audit findings

1. Core candidate ordering already ignores legacy circuit state through `runtimeOrderedCandidatesWithDecisions` / `runtimePolicyCandidates`.
2. Core credential selection still reads circuit state for `CredentialRevision <= 1` through `selectFairChannelCredential -> Iterator.SkipCircuitBreak -> IsTripped`.
3. Shared `Failover.Candidates` and `HealthFirst.Candidates` still read `PeekItemTripped` through their legacy compatibility mode, although core and Images both enter through `NewIterator` and runtime ordering.
4. Images no longer uses circuit state for admission, but still writes `RecordSuccess` / `RecordFailure` for compatibility.
5. Core relay still writes `RecordSuccess` / `RecordFailure`.
6. Managed route learning is incorrectly coupled to breaker semantics: `maybeLearnManagedRoute` is called only when `circuitFailureKindForDecision(...) == FailureHard`.
7. Credential replacement is already revision-safe: `ChannelKey.BeforeUpdate` atomically increments `credential_revision` when the secret changes. Revision 1 is the initial identity, not an unversionable permanent mode.
8. Generic/low-confidence HTTP 5xx currently remains runtime-neutral and contributes passive outlier evidence; the legacy breaker supplies a separate delayed hard gate. P4C must not silently reinterpret those failures as high-confidence availability cooldowns merely to mimic the old breaker.

## Architectural invariant after P4C

One wire outcome -> one `RoutingDecision` -> independent effects:

- current-request retry/failover/replay safety;
- runtime availability (`Available/Suspect/Cooldown/HalfOpen`);
- passive `outlierwindow` statistics;
- capability/feature suppression;
- managed-route learning, explicitly gated independently from health admission.

No scheduler or credential selector may read legacy circuit state.

---

## P4C1 — Remove circuit admission readers, preserve writes temporarily

P4C1 is deliberately narrower than the initial audit proposal. Managed-route learning is **not** changed here. The purpose of this slice is only to remove legacy circuit state from scheduling and credential admission while keeping the old writer path intact as a reversible compatibility seam.

### Task 1: RED contracts for sole admission authority

Files:
- modify `internal/relay/p4a_availability_authority_test.go`
- modify `internal/relay/balancer/p4a_availability_authority_test.go`
- update circuit-ordering contracts in `internal/relay/balancer/strategy_test.go`

Contracts:
1. A healthy revision-1 credential remains selectable even if a legacy circuit entry for the same `(channel,key,model)` is open.
2. Shared `Failover` ordering ignores legacy circuit state and orders only by priority + passive outlier health.
3. Shared `HealthFirst` ignores legacy circuit state and tiers only by passive health score.
4. Runtime cooldown still blocks the candidate independently of any legacy circuit state.

RED verification source: GitHub Actions. The new contracts must fail for the expected legacy-reader reasons before production edits.

### Task 2: Remove production admission readers

Files:
- modify `internal/relay/credential_fair.go`
- modify `internal/relay/balancer/iterator.go`
- modify `internal/relay/balancer/balancer.go`
- modify `internal/relay/balancer/health_order.go`
- modify `internal/relay/balancer/runtime_candidates.go`
- update affected tests

Implementation:
- credential admission uses `CredentialAvailableRevision` for **all** revisions;
- remove the revision-1 `SkipCircuitBreak` compatibility gate;
- delete `Iterator.SkipCircuitBreak` and its obsolete request-model field after behavior is green;
- collapse Failover to priority + passive health ordering only;
- collapse HealthFirst to passive health tiers only;
- remove the `includeLegacyCircuit` dual ordering path;
- preserve priority, health-score, tier rotation, sticky, runtime availability, concurrency, RPM, retry, replay, and route-learning behavior.

### Task 3: Replace stale live-path breaker contracts, not coverage

Two existing relay integration tests mixed useful protection with the old breaker admission mechanism. They are rewritten rather than deleted:

- multi-key fallback is seeded through authoritative credential availability cooldown instead of manually opening a circuit;
- generic HTTP 500 proves that compatibility circuit writes may still exist but cannot block a later relay admission;
- the existing dedicated 429 test continues to protect provider-model runtime cooldown behavior.

Stop gate after P4C1:
- no production scheduling/admission path calls `IsTripped`, `PeekItemTripped`, or `SkipCircuitBreak`;
- `Iterator.SkipCircuitBreak` is deleted;
- circuit writes remain intentionally present and therefore reversible;
- `RoutingDecision.CircuitEffect` remains intact;
- managed-route learning remains untouched, including its current dependency on `circuitFailureKindForDecision`;
- `circuit.go` and circuit settings remain intact;
- generic 5xx remains low-confidence/passive unless an existing high-confidence classifier chooses a runtime effect;
- full GitHub Actions CI green.

Deliverable: Draft PR for P4C1 only. Do not merge automatically.

---

## P4C2 — Decouple route learning, then remove circuit writes and policy surface

Start only after P4C1 is merged and a fresh source audit confirms zero production scheduling/admission circuit readers.

### Task 1: Decouple managed-route learning from breaker classification

Files likely include:
- `internal/relay/route_learning.go`
- `internal/relay/relay_handler.go`
- focused route-learning tests

Implementation:
- add a small route-learning eligibility policy based on the already-computed `RoutingDecision`, retry policy, and status;
- preserve the current externally observable hard/soft/ignored learning behavior;
- remove route-learning dependence on `balancer.FailureKind` / `circuitFailureKindForDecision`;
- do not add another health state machine or re-parse raw errors downstream.

Gate: route learning has an explicit independent contract before any circuit writer is removed.

### Task 2: RED contracts proving breaker writes are unnecessary

Cover core HTTP relay, Images, committed stream failure, credential failure, model capacity, provider transient, content policy, success, and managed-route learning.

The tests should assert authoritative outcomes directly:
- runtime availability state;
- credential runtime state;
- outlier effect;
- replay/failover directive;
- route-learning eligibility.

They must not require circuit state for live-path correctness.

### Task 3: Remove writers

Files likely include:
- `internal/relay/relay_attempt.go`
- `internal/relay/relay_handler.go`
- `internal/relay/images.go`
- `internal/relay/routing_decision.go`
- circuit-specific relay tests

Remove:
- `balancer.RecordSuccess` / `balancer.RecordFailure` live-path calls;
- `circuitFailureKind` / `circuitFailureKindForDecision` after route learning is independent;
- `RoutingDecision.CircuitEffect` and trace production if no compatibility consumer remains.

Stop gate after P4C2:
- zero production circuit admission readers;
- zero production circuit writers;
- route learning is independent of breaker classification;
- full CI green;
- `circuit.go` still present only as dead implementation until the final cleanup diff is reviewed.

---

## P4C3 — Delete dead breaker implementation and compatibility settings

Start only after a fresh repository-wide audit confirms zero production references.

Candidate cleanup:
- delete `internal/relay/balancer/circuit.go` and circuit-only tests;
- remove circuit reset from `internal/relay/balancer/state.go` / shared reset path;
- remove dead circuit settings from `internal/model/setting.go` and matching UI/i18n only after checking backward-compatibility expectations;
- remove obsolete circuit-specific trace/tests/comments;
- keep passive outlier/POR configuration untouched;
- keep availability cooldown policy untouched unless separately justified by production evidence.

Final gate:
- no circuit implementation, reader, writer, or active configuration surface remains;
- route learning is independently tested;
- revision-1 and later credential identities share the same availability authority;
- full GitHub Actions CI green on the exact final tree;
- no DB migration;
- no deployment in P4C.

## Explicit non-goals

- no retry-budget reduction;
- no AttemptCoordinator/P5 work;
- no cancellation-source/P6 work;
- no module-path rename;
- no POR redesign;
- no Images fairness redesign;
- no deployment.
