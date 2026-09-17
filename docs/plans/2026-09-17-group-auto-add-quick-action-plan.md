# Group Auto Add Quick Action Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Group Auto Add an idempotent one-click persisted action on every Group card while preserving the editor's unsaved preview workflow and active-preset live binding.

**Architecture:** The backend becomes authoritative for persisted Auto Add: load the saved Group, resolve matching channel-model candidates, subtract existing members, and apply the additions through the existing `GroupUpdate()` transaction so preset synchronization/cache refresh/balancer reset stay on the normal Group mutation path. The card receives a compact Wand/Sparkles action next to Copy / Preset / Protocol / Edit; the editor continues to preview Auto Add against unsaved name/regex state through a shared pure frontend helper.

**Tech Stack:** Existing Go model/op/handler layers, `regexp2.ECMAScript`, React/TanStack Query, existing Node source/pure-helper tests.

**Spec:** [`2026-09-17-group-ux-autoadd-health-audit.md`](./2026-09-17-group-ux-autoadd-health-audit.md)

## Global Constraints

- Baseline: `main@6978f0cc02770b5ca94fafd85914130471f40442` plus the completed Group UI performance slice if executed first.
- Auto Add is additive only: it never deletes or reprioritizes existing members.
- Persisted regex semantics are backend-authoritative and remain `regexp2.ECMAScript`.
- `match_regex` takes precedence over fuzzy group-name matching.
- Fuzzy fallback preserves current behavior: case-insensitive `modelName contains groupName`.
- Existing disabled channels remain eligible for membership discovery, matching the current editor inventory; runtime routing already filters disabled channels.
- The quick action must be idempotent.
- Do not revive the stale commented `/api/v1/group/auto-add-item` client hook verbatim.
- Do not create a second group-item persistence path that bypasses `GroupUpdate()` / active preset synchronization.
- No DB migration, new dependency, routing-policy change, or deployment.
- No local Mac build/deploy; repository CI remains authoritative.

---

## File Structure

**Create**

- `internal/op/group_auto_add.go`
  - persisted Group matching and additive request construction.
- `internal/op/group_auto_add_test.go`
  - authoritative matching/idempotency/order/preset-binding unit coverage.
- `internal/server/handlers/group_auto_add_test.go`
  - authenticated API contract coverage.
- `web/src/components/modules/group/autoAdd.ts`
  - pure unsaved-editor preview matcher / count helper.
- `web/tests/group-auto-add.test.mjs`
  - pure helper + frontend source contract.

**Modify**

- `internal/model/group.go`
  - `GroupAutoAddResult` API type.
- `internal/server/handlers/group.go`
  - `POST /api/v1/group/:id/auto-add`.
- `web/src/api/endpoints/group.ts`
  - result type + `useAutoAddGroupItems()` mutation and narrow Group cache update.
- `web/src/components/modules/group/Card.tsx`
  - card-level quick action.
- `web/src/components/modules/group/Editor.tsx`
  - use the extracted preview helper and show pending-match count.
- locale source(s) currently used for `group.*`
  - add concise Auto Add success/no-op/error/pending copy in English/Simplified Chinese/Traditional Chinese.

---

### Task 1: Define backend matching semantics with RED tests

**Files:**
- Create: `internal/op/group_auto_add_test.go`
- Create later: `internal/op/group_auto_add.go`

**Interfaces:**
- Planned pure resolver:

```go
func resolveGroupAutoAddCandidates(
    group model.Group,
    llms []model.LLMChannel,
) ([]model.GroupItemAddRequest, int, error)
```

Return values:

```text
[]GroupItemAddRequest = only currently-missing matches, with append priorities
int                   = total matching channel-model candidates before existing-item subtraction
error                 = invalid persisted regex or resolver failure
```

- [ ] **Step 1: Add fuzzy-match RED coverage**

Test a Group named `claude-3-5` with channel models:

```text
(channel 1, claude-3-5-sonnet) -> match
(channel 2, CLAUDE-3-5-HAIKU)   -> match
(channel 3, gpt-5)              -> no match
```

Assert case-insensitive containment and deterministic output ordering.

Use deterministic ordering:

```text
model_name ASC, then channel_id ASC
```

This avoids priority order depending on Go map iteration inside `ChannelLLMList()`.

- [ ] **Step 2: Add regex-precedence RED coverage**

For Group:

```go
Name:       "gpt"
MatchRegex: `(?i)^claude-.*-sonnet$`
```

assert that only matching Claude models are returned; Group name `gpt` must not add GPT models when regex is present.

- [ ] **Step 3: Add existing-member/idempotency RED coverage**

Given an existing `(channel_id=2, model_name=claude-3-5-sonnet)` item, assert the resolver excludes that exact key while still counting it in total matches. The new additions must start at `max(existing.Priority)+1`.

- [ ] **Step 4: Add invalid-regex RED coverage**

Compile persisted regex using:

```go
regexp2.Compile(group.MatchRegex, regexp2.ECMAScript)
```

An invalid expression must return an error and no additions. Do not silently fall back to fuzzy matching.

- [ ] **Step 5: Run focused Go test and verify RED**

```bash
go test ./internal/op -run 'TestResolveGroupAutoAdd' -count=1
```

Expected: FAIL because `resolveGroupAutoAddCandidates` does not exist.

- [ ] **Step 6: Commit RED**

```bash
git add internal/op/group_auto_add_test.go
git commit -m "test(group): define auto-add matching semantics"
```

---

### Task 2: Implement authoritative resolver and preserve GroupUpdate live binding

**Files:**
- Create: `internal/op/group_auto_add.go`
- Modify: `internal/model/group.go`
- Test: `internal/op/group_auto_add_test.go`

**Interfaces:**
- Add API result:

```go
type GroupAutoAddResult struct {
    Group          Group `json:"group"`
    Matched        int   `json:"matched"`
    Added          int   `json:"added"`
    AlreadyPresent int   `json:"already_present"`
}
```

- Add operation:

```go
func GroupAutoAddItems(groupID int, ctx context.Context) (*model.GroupAutoAddResult, error)
```

- [ ] **Step 1: Implement `resolveGroupAutoAddCandidates`**

Algorithm:

```text
1. normalize group name with strings.TrimSpace + strings.ToLower
2. if MatchRegex != "": compile regexp2.ECMAScript once
3. sort a copy of LLMChannel by Name ASC then ChannelID ASC
4. construct existing exact-key set from group.Items
5. compute nextPriority = max(group.Items.Priority)+1
6. for every matching LLMChannel:
   - increment matched
   - if exact key exists: skip addition
   - else append GroupItemAddRequest{channel_id, model_name, nextPriority, weight:1}
   - nextPriority++
7. return additions + matched
```

Do not mutate the input `llms` slice in place; copy before sorting.

- [ ] **Step 2: Implement `GroupAutoAddItems` through `GroupUpdate`**

Use current domain operations:

```go
group, err := GroupGet(groupID, ctx)
llms, err := ChannelLLMList(ctx)
adds, matched, err := resolveGroupAutoAddCandidates(*group, llms)
```

If `len(adds)==0`, return the current Group and counts without a write.

Otherwise call:

```go
updated, err := GroupUpdate(&model.GroupUpdateRequest{
    ID:         groupID,
    ItemsToAdd: adds,
}, ctx)
```

This is intentional: `GroupUpdate` owns active preset live binding through `syncActivePresetTx`, cache refresh, and balancer reset. Do **not** call `GroupItemBatchAdd` directly from this feature.

Return:

```go
&model.GroupAutoAddResult{
    Group:           *updated,
    Matched:         matched,
    Added:           len(adds),
    AlreadyPresent:  matched - len(adds),
}
```

The database uniqueness constraint/`OnConflict DoNothing` remains the concurrency safety net.

- [ ] **Step 3: Add active-preset preservation coverage**

Extend the op test with a Group that has an active preset. After `GroupAutoAddItems`, assert the preset's item snapshot is synchronized exactly as a normal Group update would be.

This test is the reason the feature must not bypass `GroupUpdate`.

- [ ] **Step 4: Run focused tests**

```bash
go test ./internal/op -run 'TestResolveGroupAutoAdd|TestGroupAutoAdd' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/model/group.go internal/op/group_auto_add.go internal/op/group_auto_add_test.go
git commit -m "feat(group): add authoritative auto-add operation"
```

---

### Task 3: Expose a narrow idempotent API

**Files:**
- Modify: `internal/server/handlers/group.go`
- Create: `internal/server/handlers/group_auto_add_test.go`

**Interfaces:**
- Route:

```text
POST /api/v1/group/:id/auto-add
Body: {}
Success data: GroupAutoAddResult
```

- [ ] **Step 1: Write handler RED tests**

Cover:

```text
[ ] invalid id -> invalid parameter response
[ ] missing group -> bounded group error, no panic
[ ] successful run -> 200 with group/matched/added/already_present
[ ] second identical run -> added == 0 and Group item count unchanged
```

Use the repository's existing authenticated handler-test setup and real op layer where practical.

- [ ] **Step 2: Run the focused handler test and verify RED**

```bash
go test ./internal/server/handlers -run 'TestGroupAutoAdd' -count=1
```

Expected: FAIL because the route/handler is absent.

- [ ] **Step 3: Register and implement the route**

Add to the existing `/api/v1/group` router:

```go
router.NewRoute("/:id/auto-add", http.MethodPost).Handle(autoAddGroupItems)
```

Handler logic:

```text
parse id
call op.GroupAutoAddItems(id, ctx)
map domain error consistently
resp.Success(c, result)
```

Do not add a second request schema; the body is intentionally empty JSON `{}`.

- [ ] **Step 4: Run focused and Group backend tests**

```bash
go test ./internal/server/handlers -run 'TestGroupAutoAdd' -count=1
go test ./internal/op ./internal/server/handlers -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/handlers/group.go internal/server/handlers/group_auto_add_test.go
git commit -m "feat(group): expose auto-add quick action"
```

---

### Task 4: Extract editor preview logic and show pending matches

**Files:**
- Create: `web/src/components/modules/group/autoAdd.ts`
- Modify: `web/src/components/modules/group/Editor.tsx`
- Create: `web/tests/group-auto-add.test.mjs`

**Interfaces:**
- Pure helper:

```ts
export type GroupAutoAddPreview = {
    matches: LLMChannel[];
    pending: LLMChannel[];
    regexError: string;
};

export function resolveGroupAutoAddPreview(args: {
    groupName: string;
    matchRegex: string;
    modelChannels: LLMChannel[];
    selectedKeys: ReadonlySet<string>;
}): GroupAutoAddPreview
```

- [ ] **Step 1: Add pure helper RED tests**

Test:

```text
- fuzzy name matching is case-insensitive
- regex overrides fuzzy matching
- selected exact channel/model keys appear in matches but not pending
- invalid browser-preview regex returns regexError and zero pending
- input array order is preserved in editor preview
```

The editor helper is a preview only; it must not be presented as the server's ECMAScript authority.

- [ ] **Step 2: Run RED**

```bash
cd web
node --test tests/group-auto-add.test.mjs
```

Expected: FAIL because `autoAdd.ts` is absent.

- [ ] **Step 3: Move current `GroupEditor` matching logic into the helper**

Preserve the current inline-flag handling for browser preview. Replace duplicated editor `useMemo` logic with the pure helper.

- [ ] **Step 4: Improve the editor action copy**

When pending matches exist, the button should expose the count without changing its one-click local behavior, e.g. localized semantics equivalent to:

```text
Auto Add · 7
```

When all matches are already selected, leave the action disabled and show `0`/the existing disabled state rather than pretending a write will occur.

- [ ] **Step 5: Run test**

```bash
cd web
node --test tests/group-auto-add.test.mjs
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/modules/group/autoAdd.ts web/src/components/modules/group/Editor.tsx web/tests/group-auto-add.test.mjs
git commit -m "refactor(group): share auto-add preview logic"
```

---

### Task 5: Add card-level Auto Add with narrow cache update

**Files:**
- Modify: `web/src/api/endpoints/group.ts`
- Modify: `web/src/components/modules/group/Card.tsx`
- Modify: `web/tests/group-auto-add.test.mjs`
- Modify: existing English/Simplified Chinese/Traditional Chinese Group locale messages.

**Interfaces:**
- Frontend result:

```ts
export interface GroupAutoAddResult {
    group: Group;
    matched: number;
    added: number;
    already_present: number;
}
```

- Hook:

```ts
export function useAutoAddGroupItems()
```

with mutation variable `{ groupId: number }`.

- [ ] **Step 1: Add source-contract RED assertions**

In `group-auto-add.test.mjs`, assert:

```js
const api = await source('../src/api/endpoints/group.ts');
const card = await source('../src/components/modules/group/Card.tsx');

assert.match(api, /\/api\/v1\/group\/\$\{groupId\}\/auto-add/);
assert.match(api, /setQueryData/);
assert.match(card, /useAutoAddGroupItems/);
assert.match(card, /WandSparkles|Sparkles/);
```

Also assert the old commented `/auto-add-item` hook is removed rather than left as misleading dead API documentation.

- [ ] **Step 2: Implement `useAutoAddGroupItems()`**

Mutation:

```ts
apiClient.post<GroupAutoAddResult>(`/api/v1/group/${groupId}/auto-add`, {})
```

On success, update only the affected Group in `['groups','list']`:

```ts
queryClient.setQueryData<Group[]>(['groups', 'list'], (old) =>
    old?.map((group) => group.id === data.group.id ? data.group : group),
);
```

Do not invalidate all model/channel/site queries. Auto Add changes only Group membership.

- [ ] **Step 3: Add the quick-action button next to existing compact actions**

Recommended order:

```text
Copy | Auto Add | Preset | Protocol | Edit
```

Use a compact Wand/Sparkles icon, the same 32 px action footprint as neighbors, tooltip, pending spinner/disabled state, and `aria-label`.

On success:

```text
added > 0 -> localized toast: added N members
added == 0 && matched > 0 -> localized toast: all matching members already present
matched == 0 -> localized toast: no matching models
```

On failure show the bounded API error via the existing Toast component.

No confirmation dialog is needed because the operation is additive and idempotent.

- [ ] **Step 4: Add three-language copy**

Add concise keys for:

```text
quickAutoAdd
quickAutoAddAdded
quickAutoAddAlreadyPresent
quickAutoAddNoMatch
quickAutoAddFailed
previewPending
```

Do not hard-code English in the card/editor.

- [ ] **Step 5: Run frontend tests**

```bash
cd web
node --test tests/*.test.mjs
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/endpoints/group.ts web/src/components/modules/group/Card.tsx web/src/components/modules/group/Editor.tsx web/src/components/modules/group/autoAdd.ts web/tests/group-auto-add.test.mjs public/locales
git commit -m "feat(group): add card auto-add action"
```

If locale files live at a different currently-authoritative path on the implementation baseline, stage those exact existing locale files rather than creating a second locale system.

---

### Task 6: Full regression and acceptance gate

- [ ] **Step 1: Backend tests**

```bash
go test ./internal/op ./internal/server/handlers -count=1
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 2: Frontend tests/lint/build through repository CI**

Required:

```text
node --test tests/*.test.mjs: PASS
lint: PASS
production build: PASS
```

- [ ] **Step 3: Interaction acceptance**

Verify:

```text
[ ] Card Auto Add works without opening Edit.
[ ] Clicking twice does not duplicate GroupItems.
[ ] Regex Group uses regex, not fuzzy group name.
[ ] Fuzzy Group adds every matching channel/model pair not already present.
[ ] Existing item priority/order is unchanged; new items append.
[ ] Active preset remains synchronized after quick Auto Add.
[ ] Editor Auto Add still operates on unsaved name/regex and does not persist until Save.
[ ] Card quick Auto Add updates the card without a full-page flicker/refetch storm.
[ ] Disabled channels may remain members but are not routed while disabled.
```

- [ ] **Step 4: Final repository CI gate**

Required:

```text
governance: PASS
backend Vet/full tests: PASS
frontend lint/test/build: PASS
```

- [ ] **Step 5: Record actual commit/CI evidence in this plan only after the gate is green**

No deployment is implied by merge readiness.