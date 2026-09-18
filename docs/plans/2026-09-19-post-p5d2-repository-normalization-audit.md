# Post-P5D2 Repository Normalization Audit

> Baseline: `main@a487ffbadc2fbf8652f66e83891e7788173e15c3`
>
> Verified baseline CI: `35365675975` — governance, backend and frontend successful.
>
> Scope: normalize repository truth, documentation, governance guards and stale implementation comments without changing relay behavior, database schema, deployment state, or production containers.

## 1. Audit conclusion

P5D2 itself is merged and verified. The repository is structurally healthy, but several layers had drifted behind the actual implementation:

1. public/repository identity had partially moved to ZyRealm while governance/manual-build paths still referenced `mumu-140/octopus-concurrency`;
2. the release workflow published `ghcr.io/mumu-140/zyrealm`, while the manual production build script still stamped the old image namespace and source URL;
3. `USAGE*.md` still documented the retired configurable Circuit Breaker, old 401 behavior, and cost-based multi-Key selection;
4. `README*.md` described a hard wire-attempt maximum of 20 even though the backend only requires a positive integer and the UI says there is no application-level maximum;
5. CI still marked `go vet` as non-gating because of a historical copylocks note, although the verified P5D2 main baseline currently passes Vet;
6. production source comments still referenced temporary P5A/P5 migration phases after those slices had completed;
7. older roadmaps used words such as “current” and “NEXT” from their original baselines without a directory-level current-status index.

These are normalization defects, not reasons to reopen routing policy.

## 2. Normalization performed

### Repository and release identity

- canonical GitHub repository identity: `mumu-140/ZyRealm`;
- future production image/source metadata: `ghcr.io/mumu-140/zyrealm` and `https://github.com/mumu-140/ZyRealm`;
- `production-state.json.repository.github` updated to the current repository identity;
- live production image/container fields intentionally left unchanged;
- production manual now explicitly separates current product/repository identity from a still-running historical image identity.

### Governance

- `go vet ./...` restored as a hard CI gate;
- governance script now verifies:
  - machine-readable GitHub repository identity;
  - AGENTS/manual ZyRealm identity;
  - manual production build image namespace and source URL;
  - release workflow GHCR namespace.

### User-facing routing documentation

- removed active Circuit Breaker guidance;
- documented provider/provider×model/credential runtime cooldown + passive half-open recovery;
- documented marker-first retry direction and credential rotation;
- documented provider-local equal-weight credential fairness rather than historical `TotalCost` selection;
- corrected request-budget defaults/validation semantics;
- updated issue links to `mumu-140/ZyRealm`.

### Source comments

Removed completed-phase wording such as `P5A`, `P5`, “later slices”, and “legacy breaker entry” from live relay comments while preserving the architectural contracts.

### Plan organization

- added `docs/plans/README.md` as the live status/index layer;
- marked the old gap roadmap and adaptive-routing design documents as historical snapshots;
- preserved historical content rather than rewriting old design evidence to look current.

## 3. Deliberately not changed

The audit found real behavioral asymmetries that require a separate design decision:

### Sidepath attempt budgets

- Core HTTP: shared request-local provider/wire budgets;
- Images: existing total upstream-start ceiling derived from group retry settings;
- Compact/WS: existing same-channel retry ceilings.

This is a semantic difference, not a naming cleanup. It remains deferred.

### Capacity and RPM admission

- Core HTTP and Images enforce channel concurrency/RPM admission;
- Compact and WS do not.

Adding those gates could change availability and throughput and therefore requires dedicated RED/GREEN coverage.

### Compatibility surfaces

The following are intentionally not renamed merely for branding consistency:

- Go module/import path `github.com/bestruirui/octopus`;
- `OCTOPUS_*` environment variables;
- established binary/container/deployment directory names where renaming could break compatibility;
- the currently running historical image reference recorded in live production state.

## 4. Safety boundary

This audit does **not** authorize or perform:

- database migrations;
- production SQLite writes;
- container restart/recreate;
- Compose cutover;
- release/tag creation;
- live image replacement.

Only repository text/code/governance changes are in scope.

## 5. Merge gate

Before merge:

1. branch must remain based on the audited `main` with no unrelated changes;
2. governance CI must pass with the new identity assertions;
3. backend CI must pass with `go vet` now hard-gating;
4. full Go tests must pass;
5. frontend lint/test/build must pass;
6. final diff must contain no relay-policy change.

After merge, run the same merged-tree CI gate before declaring normalization complete.

## 6. Recommended next engineering audit

Do not start another routing implementation slice automatically.

The next source audit should decide whether the product actually needs:

1. shared provider/wire attempt budgets on Images/Compact/WS;
2. concurrency/RPM admission on Compact/WS;
3. neither, because those endpoints intentionally have different transport/replay economics.

That decision should be based on real workload/failure evidence and endpoint semantics, not symmetry alone.
