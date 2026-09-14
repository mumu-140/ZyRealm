# P0.2 Global Model Filter Implementation Plan

> Status: source-audited and implementation-ready; no production code has been changed.
>
> Baseline: ZyRealm `main@786c1c6c94a2228d47b971e2720b86523a091ba5` (2026-09-14).
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

The result is an intersection, not an override or union. Provider model ordering must remain stable.

This slice is about **discovery/sync admission**, not request-time routing. It must not add a second model-routing policy or alter relay failover behavior.

## Source audit

### 1. Existing global settings are already schema-free

`internal/model/setting.go`, `internal/op/setting.go`, and `internal/server/handlers/setting.go` already persist arbitrary key/value settings through the settings table and cache.

Therefore the global filter should use the existing setting key:

```text
model_filter_regex
```

No database migration or new table is required. The empty string means disabled.

The backend settings update path already calls `Setting.Validate()` before persistence, so regex syntax validation belongs on that authoritative path. The UI may provide convenience feedback, but it must not be the only validator.

### 2. Per-channel filtering already has the right regex engine

`internal/helper/fetch.go` currently applies `channel.MatchRegex` after parsing provider `/models` responses. It compiles with:

```go
regexp2.Compile(pattern, regexp2.RE2)
```

This is the established ZyRealm channel-filter behavior. P0.2 should reuse the same engine and matching semantics for the global filter rather than introduce Go `regexp` or JavaScript regex as a second backend dialect.

`github.com/dlclark/regexp2 v1.11.5` is already a direct dependency; no dependency change is needed.

### 3. Model discovery has more entry points than upstream Octopus

The upstream commit adds `model_filter_regex`, filters the manual handler result globally and then applies channel `MatchRegex`. That semantic contract is sound, but the patch location is too narrow for ZyRealm.

Current ZyRealm discovery/update paths include:

- manual channel model fetch: `internal/server/handlers/channel.go`;
- batch channel refresh: `internal/server/handlers/channel_batch.go`;
- scheduled channel sync: `internal/task/sync.go`;
- managed-site/token discovery and fallback discovery: `internal/sitesync/sync_fetch.go`;
- managed-site projection into channels: `internal/sitesync/project.go`.

A handler-only patch would allow scheduled sync or site fallback discovery to bypass the global policy.

### 4. Scheduled sync persists the filtered result

`internal/task/sync.go` does more than display fetched models. It diffs the new list against persisted `channel.Models`, removes obsolete group items, applies auto-add behavior, saves the channel, and refreshes the channel cache.

Therefore changing the global filter intentionally affects the next scheduled/manual refresh:

- newly excluded models may disappear from persisted `channel.Models`;
- their existing group items may be removed by the current sync cleanup path;
- sync reports should continue to expose those removals through the existing added/removed model reporting.

This is expected policy enforcement, not a cache-only presentation filter.

### 5. Managed-site projection must not bypass the global rule

Regular scheduled/batch channel sync already skips `site.IsManagedProjectionChannel(...)`, because site sync owns those channels.

`internal/sitesync/sync_fetch.go` first tries `helper.FetchModels`, but some platforms/failure cases fall back to `/api/available_model`, managed-session snapshots, Sub2API endpoints, or other site-specific model sources. Those fallback lists can bypass `helper.FetchModels` entirely.

Therefore P0.2 must filter the **final discovered site-model list before it becomes authoritative projection input**, including fallback results. It must not make regular channel sync start editing managed projection channels.

Managed ownership stays unchanged:

```text
regular channel -> channel sync owns Models
managed projection channel -> site sync owns Models
both paths -> global discovery admission applies
```

The existing per-channel `MatchRegex` is not copied into site-owned projection metadata. Managed projections receive the global rule at discovery/projection ingress; their ownership model remains intact.

### 6. Setting updates must not trigger a surprise bulk rewrite

Channel models are cached as part of the existing channel cache. There is no separate discovered-model cache that needs explicit invalidation when the setting changes.

Saving `model_filter_regex` should update the settings store/cache only. It should **not** immediately rewrite every persisted channel or launch a full-site/full-channel resync.

The new policy takes effect on the next relevant operation:

- manual model fetch;
- batch model refresh;
- scheduled model sync;
- managed-site model sync.

This limits blast radius and keeps existing task/reporting paths responsible for model additions/removals. The UI must state this behavior clearly.

### 7. Invalid regex must stop mutation, not fail open or erase models

API-side validation should reject invalid `model_filter_regex` before it is persisted.

Runtime filtering must still return an error if an invalid value is encountered, for example after manual database editing or legacy corruption. A refresh/sync encountering that error must leave existing persisted models intact. It must not:

- accept all models as a fallback;
- silently turn the invalid expression into an empty result;
- persist an empty model list because compilation failed.

## Chosen architecture

### Shared pure matcher

Create a dependency-light package:

```text
internal/utils/modelmatch/
```

It owns the backend regex contract and has no dependency on `model`, `op`, handlers, tasks, or site sync.

Proposed API:

```go
func Validate(pattern string) error
func Filter(models []string, patterns ...string) ([]string, error)
```

Rules:

- trim only the pattern for the disabled/empty check; do not rewrite model names;
- compile each non-empty pattern with `regexp2.RE2`;
- a model must match every non-empty pattern;
- preserve input order;
- do not add a new deduplication policy;
- return compile/match errors rather than silently widening the result.

`Setting.Validate()` uses `modelmatch.Validate` for `model_filter_regex`.

`helper.FetchModels` refactors its existing channel regex block to call `modelmatch.Filter(models, channel.MatchRegex)`. This keeps channel behavior centralized on the same matcher.

Global configuration is **not** read from inside `helper` or `modelmatch`. Callers read `op.GetSetting("model_filter_regex")` and pass the value explicitly to filtering orchestration. This avoids a hidden global dependency and prevents helper/op package coupling.

### Global application points

For normal channels:

```text
helper.FetchModels(...)
  -> existing channel MatchRegex via modelmatch
caller
  -> modelmatch.Filter(models, globalRegex)
  -> existing mapping/diff/persistence logic
```

Sequential filtering is equivalent to `global AND channel` and keeps current `FetchModels` behavior stable for callers/tests that do not opt into a global setting.

For managed-site discovery:

```text
primary site fetch OR fallback/session/Sub2API result
  -> normalize using existing site rules
  -> modelmatch.Filter(finalModels, globalRegex)
  -> existing authoritative/fallback decision
  -> existing projection
```

The global filter should be applied at a common site-discovery boundary so every source, including fallbacks, is covered once.

## UI/API design

Use the existing settings API; no new HTTP endpoint is required.

Frontend changes:

- add `model_filter_regex` to `web/src/api/endpoints/setting.ts` and `SettingKey`;
- place the control in `web/src/components/modules/setting/SyncTasks.tsx`, because it governs model discovery/synchronization rather than relay request execution;
- use the existing setting field/save primitives;
- add `setting.syncTasks.modelFilter.*` translations in:
  - `web/public/locale/zh_hans.json`;
  - `web/public/locale/zh_hant.json`;
  - `web/public/locale/en.json`.

UI copy must communicate:

- empty = disabled;
- global and channel filters are both required when both exist;
- the setting affects subsequent fetch/refresh/sync operations;
- saving it does not immediately purge existing models.

Do not add a browser-side JavaScript regex engine as the policy authority. The existing channel-form local `new RegExp(...)` preview predates this slice and is explicitly out of scope; backend `regexp2.RE2` remains authoritative.

## Implementation plan

### Task 1 — Lock the composed matcher with RED tests

Files:

- Create `internal/utils/modelmatch/modelmatch.go`.
- Create `internal/utils/modelmatch/modelmatch_test.go`.
- Modify `internal/helper/fetch.go`.
- Extend `internal/helper/fetch_test.go` only where a fetch-level regression is needed.

RED tests first:

1. empty patterns return models unchanged and in order;
2. global-only pattern filters correctly;
3. channel-only pattern filters correctly;
4. global + channel patterns produce the intersection;
5. input order is preserved;
6. invalid pattern returns an error;
7. existing `FetchModels` channel `MatchRegex` behavior remains equivalent after refactor.

Run targeted RED/GREEN:

```bash
go test -buildvcs=false ./internal/utils/modelmatch ./internal/helper -count=1
```

Minimal implementation: `regexp2.RE2`, no new dependency and no extra matching language.

### Task 2 — Add authoritative setting validation

Files:

- Modify `internal/model/setting.go`.
- Create or extend `internal/model/setting_test.go`.

RED tests:

- empty `model_filter_regex` is valid;
- valid RE2-compatible expression is valid;
- invalid expression is rejected before persistence;
- unrelated settings keep existing validation behavior.

Implementation:

- add default `model_filter_regex = ""` through the existing default settings mechanism;
- route only this key through `modelmatch.Validate`;
- do not add migration/schema code.

Run:

```bash
go test -buildvcs=false ./internal/model -count=1
```

### Task 3 — Apply the global filter to normal channel discovery

Files:

- Modify `internal/server/handlers/channel.go`.
- Modify `internal/server/handlers/channel_batch.go`.
- Modify `internal/task/sync.go`.
- Add focused regression tests in the corresponding packages; create narrowly named `*_test.go` files if no suitable file exists.

Behavior to lock with RED tests:

- manual fetch response contains only models passing global and channel filters;
- batch refresh persists only globally admitted models;
- scheduled sync treats newly filtered-out persisted models as normal removals through the existing diff/report path;
- invalid runtime global regex aborts refresh/sync and does not mutate existing models;
- empty global filter is behaviorally identical to current main.

Read the setting once per operation where practical, then pass it explicitly rather than making `helper` read global state.

Suggested targeted run:

```bash
go test -buildvcs=false ./internal/server/handlers ./internal/task -count=1
```

### Task 4 — Cover managed-site primary and fallback discovery

Files:

- Modify `internal/sitesync/sync_fetch.go`.
- Extend existing site-sync tests or create `internal/sitesync/sync_fetch_test.go`.
- Modify `internal/sitesync/project.go` only if tests prove a final projection guard is required; do not duplicate filtering by default.

RED cases:

- models returned through `helper.FetchModels` are globally filtered;
- OneHub/DoneHub `available_model` fallback is globally filtered;
- managed-session fallback is globally filtered;
- Sub2API discovery is globally filtered;
- a filter that legitimately matches zero models remains an authoritative empty discovery when the upstream source was authoritative;
- an invalid regex returns an error/non-authoritative outcome and preserves historical projection state;
- regular channel-sync skip behavior for managed projections remains unchanged.

Important distinction: **valid filter -> zero results** is policy; **invalid filter -> error** is not an authoritative empty model set.

Run:

```bash
go test -buildvcs=false ./internal/sitesync -count=1
```

### Task 5 — Add the Sync Tasks setting UI

Files:

- Modify `web/src/api/endpoints/setting.ts`.
- Modify `web/src/components/modules/setting/SyncTasks.tsx`.
- Modify `web/public/locale/zh_hans.json`.
- Modify `web/public/locale/zh_hant.json`.
- Modify `web/public/locale/en.json`.

Implementation:

- add `SettingKey.ModelFilterRegex`;
- render a text input/setting row near model sync controls;
- save through the existing setting mutation;
- describe intersection and next-sync semantics;
- surface backend validation errors through existing error/toast behavior rather than inventing a second regex validator.

Frontend gate:

```bash
cd web
pnpm test
pnpm lint
pnpm build
```

No new frontend dependency is expected.

### Task 6 — Full regression and boundary audit

Run on the final implementation head:

```bash
go test -buildvcs=false ./internal/utils/modelmatch ./internal/helper ./internal/model ./internal/server/handlers ./internal/task ./internal/sitesync -count=1
go vet ./...
go test -buildvcs=false ./...
cd web && pnpm test && pnpm lint && pnpm build
```

Then require GitHub Actions governance/backend/frontend all green on the exact final head.

Diff audit must confirm:

- no DB migration;
- no dependency change;
- no relay routing/failover change;
- no automatic bulk resync on setting save;
- no P0.3/P1 code;
- managed projection ownership remains with site sync;
- global regex is applied to every audited discovery source, including site fallbacks.

## Expected implementation surface

Core production files are expected to stay within:

```text
internal/utils/modelmatch/*
internal/model/setting.go
internal/helper/fetch.go
internal/server/handlers/channel.go
internal/server/handlers/channel_batch.go
internal/task/sync.go
internal/sitesync/sync_fetch.go
web/src/api/endpoints/setting.ts
web/src/components/modules/setting/SyncTasks.tsx
web/public/locale/{zh_hans,zh_hant,en}.json
```

Tests may add adjacent `*_test.go` files. `internal/sitesync/project.go` is conditional only if a missing final-boundary invariant is proven by RED tests.

## Explicit non-goals

- No request-time model routing filter.
- No new database table or migration.
- No automatic global resync when the setting changes.
- No blanket deletion of already persisted models at setting-save time.
- No change to channel `MatchRegex` meaning.
- No replacement of managed-site projection ownership.
- No attempt to standardize the channel form's existing JavaScript preview regex with backend `regexp2` in this slice.
- No P0.3 live-request/manual-interrupt work.
- No P1 credential × model × protocol schema work.

## Completion gate

P0.2 is complete only when:

- the global setting is backend-validated;
- global + channel filtering is proven as intersection with stable ordering;
- manual, batch, scheduled, managed-site primary, and managed-site fallback discovery are all covered;
- invalid runtime configuration cannot mutate persisted models;
- the UI clearly states delayed next-sync enforcement;
- targeted tests, full Go suite/Vet, frontend test/lint/build, governance, and final-head GitHub Actions are green;
- the merged `main` tree is re-verified before P0.3 source audit begins.
