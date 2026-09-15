# P0.2 Global Model Filter — Source Audit, Implementation Plan, and Completion Record

> Status: **MERGED + VERIFIED**.
>
> Source-audit baseline: ZyRealm `main@786c1c6c94a2228d47b971e2720b86523a091ba5`.
>
> Implementation PR: `#22 feat(model): add global model discovery filter`.
>
> Squash merge commit: `fe61b4dd7649a5b45de2fec19e2806ad74704830`.
>
> Merged-tree CI: `#541`, run `34926801554`, all governance/backend/frontend jobs successful.
>
> Upstream reference: Octopus `d5a893ff124ca1cb56f2eb25e14380347d247ae9`.

## Goal

Add one system-level model-discovery regex that composes with the existing channel-level `MatchRegex`.

A discovered model is eligible only when it satisfies every configured filter:

```text
upstream discovered models
  -> global model_filter_regex, if non-empty
  -> channel MatchRegex, if non-empty
  -> existing model mapping / persistence / projection logic
```

The result is an intersection, not an override or union. Provider model ordering remains stable.

This slice governs discovery/sync admission only. It does not introduce request-time model routing policy and does not alter relay failover behavior.

## Source-audit corrections

The initial docs-only source-audit PR `#21` was useful for scoping, but two assumptions were corrected before and during implementation.

### 1. Regex dialect is ECMAScript, not RE2

The draft plan incorrectly stated that current ZyRealm channel filtering used:

```go
regexp2.Compile(pattern, regexp2.RE2)
```

Fresh inspection showed that both current ZyRealm `Channel.MatchRegex` behavior and the referenced Octopus global filter use:

```go
regexp2.ECMAScript
```

P0.2 therefore preserves the existing channel regex dialect and centralizes both global and channel filtering on `regexp2.ECMAScript`.

No new regex dependency or second backend regex language was introduced.

### 2. Managed-channel ownership is more nuanced than the draft plan stated

The draft plan described regular scheduled/batch channel sync as if both paths simply skipped managed projection channels. The actual code has different pre-existing ownership gates:

- scheduled sync continues to rely on the existing `ChannelUpdate` managed-channel read-only guard;
- batch refresh continues to use its existing `BypassManagedCheck` path;
- site sync continues to own site-model discovery/projection and receives the global filter inside that workflow.

P0.2 does not broaden or redesign those ownership semantics. It applies the global admission rule at the audited discovery boundaries while leaving the pre-existing mutation/ownership rules intact.

## Chosen architecture

### Shared pure matcher

P0.2 adds:

```text
internal/utils/modelmatch/
```

The matcher is dependency-light and owns the backend regex contract:

```go
func Validate(pattern string) error
func Filter(models []string, patterns ...string) ([]string, error)
```

Rules:

- empty pattern means disabled;
- compile non-empty patterns with `regexp2.ECMAScript`;
- every configured pattern must match;
- preserve input order;
- do not add a new deduplication policy;
- return compile/match errors instead of silently widening or erasing results.

Existing `helper.FetchModels` channel-level `MatchRegex` filtering now uses the same shared matcher, preserving existing channel semantics while removing duplicated regex logic.

### Global setting

The global filter uses the existing schema-free settings mechanism:

```text
model_filter_regex
```

The default is an empty string.

`Setting.Validate()` validates the regex before persistence. No database migration, table, or schema change is required.

Saving the setting updates the existing setting store/cache only. It does **not** trigger an immediate bulk rewrite of persisted channel models and does not launch a global resync.

The setting takes effect on the next relevant discovery/sync operation.

## Enforced discovery paths

### Normal channel discovery

The global filter is applied to the audited normal-channel flows:

- manual model fetch;
- batch model refresh;
- scheduled model sync.

The existing per-channel `MatchRegex` remains in `helper.FetchModels`; the global result is then applied by the operation that owns the global setting. Sequential filtering is equivalent to the required `global AND channel` intersection.

Existing diff/report/group-item cleanup behavior remains responsible for additions and removals after a later refresh/sync.

### Managed-site discovery

ZyRealm has site-specific discovery paths that can bypass ordinary `helper.FetchModels`, including grouped primary sources, managed-session fallbacks, site-specific fallbacks, Sub2API/direct-token paths, and other site-model sources.

P0.2 adds a common site-model admission layer and a final direct-snapshot guard so the global regex applies before authoritative site models are persisted/projected.

Important semantic distinction:

- valid regex + zero matches = authoritative policy result when the upstream discovery itself was authoritative;
- invalid regex = error/non-authoritative result and historical models must be preserved.

This prevents an invalid runtime value, including a value introduced outside the normal validated settings API, from being misinterpreted as an authoritative empty model set.

## UI/API behavior

No new HTTP endpoint is required. The existing settings API is reused.

The Sync Tasks UI exposes `model_filter_regex` and documents that:

- empty means disabled;
- global and channel regexes are an intersection when both are configured;
- the rule applies to subsequent fetch/refresh/sync operations;
- saving the setting does not immediately purge existing models.

English, Simplified Chinese, and Traditional Chinese locale copy was added.

No browser-side regex validator is treated as policy authority. Backend validation remains authoritative.

## TDD implementation record

### Task 1 — Shared matcher and channel-regex characterization

RED contract established the required matcher API and intersection semantics before production implementation.

Evidence:

- RED CI `#480`, run `34868681961`: new matcher tests failed because `Filter` / `Validate` did not exist;
- GREEN CI `#482`, run `34868938034`: governance, backend Vet/full tests, frontend lint/test/build successful.

The implementation also added a characterization regression around the existing `FetchModels + MatchRegex` behavior before routing that logic through the shared matcher.

### Task 2 — Authoritative setting validation

Added empty default and backend validation for `model_filter_regex` through the existing `Setting.Validate()` path.

Invalid expressions are rejected before normal persistence.

### Task 3 — Normal discovery/sync integration

Global admission was wired into:

- manual channel fetch;
- batch refresh;
- scheduled model sync.

Focused tests cover intersection behavior, persisted updates, scheduled-sync removal/report behavior, invalid runtime regex handling, and empty-global-filter compatibility.

### Task 4 — Managed-site primary/fallback integration

The site-sync layer applies the global filter across grouped primary/fallback discovery and direct-token snapshot admission.

Tests cover primary/fallback sources, valid-zero policy, invalid-regex non-authoritative behavior, and preservation of historical models.

### Task 5 — Sync Tasks UI

UI contract was written RED first.

Evidence:

- RED CI `#520`, run `34923232934`: frontend lint passed; only the two new UI contract tests failed for the intentionally missing setting/control/locales;
- GREEN CI `#536`, run `34924185389`: governance, backend, and frontend jobs all successful.

### Task 6 — Final review and regression gate

A final review removed a production wrapper that existed only to make a test seam and moved that seam into `_test.go`, reducing production surface without changing behavior.

Final PR head:

```text
834fec88cf2a24f28b29b0c708913df71d843071
```

Final PR-triggered CI:

```text
#540 / 34924619996
```

Result:

- governance: success;
- backend `go vet ./...`: success;
- backend full Go tests: success;
- frontend lint: success;
- frontend tests: success;
- frontend production build: success.

## Merge and merged-tree verification

PR `#22` was squash-merged to `main` as:

```text
fe61b4dd7649a5b45de2fec19e2806ad74704830
```

The merged-tree push CI was then re-run on that exact `main` commit:

```text
CI #541 / run 34926801554
```

Result:

- governance: success;
- backend Vet: success;
- backend full Go tests: success;
- frontend lint/test/build: success.

This satisfies the final P0.2 merge gate.

## Final implementation surface

Production changes are limited to the audited setting/matcher/discovery/UI paths, including:

```text
internal/utils/modelmatch/*
internal/model/setting.go
internal/helper/fetch.go
internal/server/handlers/channel.go
internal/server/handlers/channel_batch.go
internal/task/sync.go
internal/sitesync/core.go
internal/sitesync/model_filter.go
internal/sitesync/sync_fetch.go
web/src/api/endpoints/setting.ts
web/src/components/modules/setting/SyncTasks.tsx
web/public/locale/{zh_hans,zh_hant,en}.json
```

Adjacent tests cover the new semantics.

## Boundary audit

Confirmed at final PR head and merged tree:

- no DB migration or new table;
- no dependency / `go.mod` / `go.sum` change;
- no relay routing/failover change;
- no automatic bulk resync on setting save;
- no P0.3 live-request/manual-interrupt code;
- no P1 credential × model × protocol schema work;
- no change of existing channel `MatchRegex` dialect;
- no replacement of existing managed-site projection ownership.

## Completion gate

P0.2 is **MERGED + VERIFIED** because:

- the global setting is backend-validated;
- global + channel filtering is proven as an intersection with stable ordering;
- manual, batch, scheduled, managed-site primary, fallback, and direct-token discovery paths are covered;
- invalid runtime configuration does not become an authoritative empty set that erases historical models;
- the UI states delayed next-sync enforcement;
- final-head CI and merged-tree CI are both green.

P0.3 may now begin only with a fresh source audit against the current `main`. No P0.3 implementation should be inferred from the older remembered roadmap scope.
