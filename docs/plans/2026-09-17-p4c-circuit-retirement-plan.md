# P4C Circuit Retirement Plan

## Goal

Make `internal/relay/availability` the sole immediate routing-health authority without silently changing replay, credential, route-learning, outlier, or managed-site behavior.

P4C is intentionally staged. Do **not** delete `internal/relay/balancer/circuit.go` until production circuit readers are gone and circuit writes are proven inert.

Current baseline: `main@7fc188c1b08a347179d3a2efda1f84a148b29c3d`.

## Fresh source-audit findings

1. Core candidate ordering already ignores legacy circuit state through `runtimeOrderedCandidatesWithDecisions` / `runtimePolicyCandidates`.
2. Core credential selection still reads circuit state for `CredentialRevision <= 1` through `selectFairChannelCredential -> Iterator.SkipCircuitBreak -> IsTripped`.
3. Shared `Failover.Candidates` and `HealthFirst.Candidates` still read `PeekItemTripped` through their legacy compatibility mode, although core/Images both enter through `NewIterator` and runtime ordering.
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

## P4C1 — Remove circuit readers, preserve writes temporarily

### Task 1: RED contracts for sole admission authority

Files:
- modify `internal/relay/p4a_availability_authority_test.go`
- modify `internal/relay/balancer/p4a_availability_authority_test.go`
- add/modify focused route-learning policy tests near `internal/relay/route_learning.go`

Contracts:
1. A healthy revision-1 credential remains selectable even if a legacy circuit entry for the same `(channel,key,model)` is open.
2. Shared `Failover` ordering ignores legacy circuit state and orders only by priority + passive outlier health.
3. Shared `HealthFirst` ignores legacy circuit state and tiers only by passive health score.
4. Runtime cooldown still blocks the candidate independently of any legacy circuit state.
5. Managed-route learning eligibility preserves the current hard/soft/ignored behavior without using `balancer.FailureKind` or circuit state.

RED verification source: GitHub Actions. The new contracts must fail for the expected legacy-reader/coupling reasons before production edits.

### Task 2: Decouple managed-route learning from breaker classification

Files:
- modify `internal/relay/route_learning.go`
- modify `internal/relay/relay_handler.go`
- add/update focused tests

Implementation:
- introduce a small route-learning eligibility helper based on the already-computed `RoutingDecision`, retry policy, and status;
- preserve the existing external behavior of the old `FailureHard` gate;
- call `maybeLearnManagedRoute` using this independent predicate;
- do not add a new health state machine or parse raw errors twice.

Gate: route learning must no longer depend on `circuitFailureKindForDecision`.

### Task 3: Remove production circuit reads

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
- remove `Iterator.SkipCircuitBreak` if no remaining caller exists;
- collapse `failoverCandidates(..., includeLegacyCircuit)` to circuit-free ordering;
- collapse `healthFirstCandidates(..., includeLegacyCircuit, ...)` to circuit-free ordering;
- preserve priority, health-score, tier rotation, sticky, runtime availability, concurrency, and RPM behavior.

Stop gate after P4C1:
- no production scheduling/admission path calls `IsTripped`, `PeekItemTripped`, or `SkipCircuitBreak`;
- circuit writes remain intentionally present and therefore reversible;
- `circuit.go` is not deleted;
- generic 5xx remains low-confidence/passive unless an existing high-confidence classifier chooses a runtime effect;
- full GitHub Actions CI green.

Deliverable: Draft PR for P4C1 only. Do not merge automatically.

---

## P4C2 — Remove circuit writes and policy surface

Start only after P4C1 is merged and a fresh source audit confirms zero production circuit readers.

### Task 1: RED contracts proving breaker writes are unnecessary

Cover core HTTP relay, Images, committed stream failure, credential failure, model capacity, provider transient, content policy, and success.

The tests should assert the authoritative outcomes directly:
- runtime availability state;
- credential runtime state;
- outlier effect;
- replay/failover directive;
- route-learning eligibility.

They must not assert circuit state.

### Task 2: Remove writers

Files likely include:
- `internal/relay/relay_attempt.go`
- `internal/relay/relay_handler.go`
- `internal/relay/images.go`
- `internal/relay/routing_decision.go`
- circuit-specific relay tests

Remove:
- `balancer.RecordSuccess` / `balancer.RecordFailure` live-path calls;
- `circuitFailureKind` / `circuitFailureKindForDecision` once route learning is independent;
- `RoutingDecision.CircuitEffect` and trace production if no compatibility consumer remains.

Stop gate after P4C2:
- zero production circuit readers;
- zero production circuit writers;
- full CI green;
- `circuit.go` still present only as dead implementation until the final cleanup diff is reviewed.

---

## P4C3 — Delete dead breaker implementation and compatibility settings

Start only after a fresh repository-wide audit confirms zero production references.

Candidate cleanup:
- delete `internal/relay/balancer/circuit.go` and circuit-only tests;
- remove circuit reset from `internal/relay/balancer/state.go`;
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
