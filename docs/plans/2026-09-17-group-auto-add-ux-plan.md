# Group Auto Add UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** make per-group Auto Add available directly from each group card header, beside Copy / Preset / Protocol Policy, while preserving the editor's existing matching semantics and requiring an explicit preview/confirm before persistence.

**Architecture:** extract the editor's current name/regex matching into one pure helper, compute card-level candidate resolutions from the Group page's shared model-channel snapshot introduced by the UI-stability work, reuse the same helper inside the editor, and persist card-header additions through the existing `GroupUpdateRequest.items_to_add` path. Do not reuse the separate channel/global Auto Group subsystem.

**Tech Stack:** Next.js 16, React 19, TanStack Query, Radix/shadcn popover primitives, next-intl, Node test runner.

**Spec:** `docs/plans/2026-09-17-group-ui-auto-add-health-audit.md`

**Dependency:** land `2026-09-17-group-ui-stability-plan.md` first so Group cards consume one shared page-level model snapshot instead of introducing another per-card broad query.

## Global Constraints

- Implementation branch starts from fresh `main` after the Group UI stability slice is merged.
- No backend production change, DB migration, protocol/routing change, or new Auto Group subsystem.
- The card shortcut means **per-group candidate addition**, not channel/global Auto Group orchestration.
- First click never mutates; show a preview and require explicit confirmation.
- Preserve current editor matching semantics unless a separate behavior change is approved.
- Duplicate protection happens both client-side and through the existing backend unique/conflict-safe path.
- Default new member weight remains `1`.
- New member priority is deterministic and appended after the group's current maximum priority.
- Invalid regex is a user-visible non-mutating state, not an exception/toast loop.
- No local Mac build/install/deploy.

---

## File Structure

**Create**
- `web/src/components/modules/group/auto-add.ts` — pure matching/resolution and request-building helpers.
- `web/src/components/modules/group/AutoAddPopover.tsx` — card-header preview/confirm UI.
- `web/src/provider/group-auto-add-messages.ts` — feature-scoped `en` / `zh_hans` / `zh_hant` messages.
- `web/tests/group-auto-add.test.mjs` — pure behavior + source-contract tests.

**Modify**
- `web/src/components/modules/group/Editor.tsx` — replace inline matching logic with the shared helper.
- `web/src/components/modules/group/index.tsx` — derive card Auto Add resolutions from the existing shared model-channel snapshot.
- `web/src/components/modules/group/Card.tsx` — render the new header action beside Copy / Preset / Protocol Policy.
- `web/src/provider/locale.tsx` — merge the feature-scoped messages into the existing next-intl catalog.

---

### Task 1: Extract and lock Auto Add matching semantics

**Files:**
- Create: `web/src/components/modules/group/auto-add.ts`
- Create: `web/tests/group-auto-add.test.mjs`

**Interfaces:**

```ts
import type { Group, GroupItemAddRequest } from '@/api/endpoints/group';
import type { LLMChannel } from '@/api/endpoints/model';

export type GroupAutoAddBasis = 'name' | 'regex';

export type GroupAutoAddResolution = {
  basis: GroupAutoAddBasis;
  candidates: LLMChannel[];
  regexError?: string;
};

export function resolveGroupAutoAddCandidates(
  group: Pick<Group, 'name' | 'match_regex' | 'items'>,
  modelChannels: readonly LLMChannel[],
): GroupAutoAddResolution;

export function buildGroupAutoAddItems(
  group: Pick<Group, 'items'>,
  candidates: readonly LLMChannel[],
): GroupItemAddRequest[];
```

Matching behavior must be copied from the current `GroupEditor`, not reinterpreted:

1. When `group.match_regex` is non-empty, compile it and match against `LLMChannel.name`.
2. Otherwise normalize the group name using the existing name-matching helper and apply the existing `matchesGroupName(...)` semantics.
3. Exclude any `(channel_id, model_name)` already present in `group.items`.
4. Preserve the input `modelChannels` order after filtering.
5. On invalid regex return `{ basis: 'regex', candidates: [], regexError }`; do not throw.

`buildGroupAutoAddItems()` must:

- start priorities at `max(existing priority) + 1`;
- increment by one in candidate order;
- set `weight: 1`;
- produce no duplicate `(channel_id, model_name)` pairs.

- [ ] **Step 1: Write RED behavior tests**

Cover at least:

```js
test('name mode reuses existing group-name semantics', ...);
test('regex mode matches model names', ...);
test('invalid regex is returned as a non-mutating error', ...);
test('existing channel-model members are excluded', ...);
test('new priorities append deterministically and weight defaults to one', ...);
test('candidate ordering follows the model-channel snapshot', ...);
```

Representative request assertion:

```js
assert.deepEqual(
  buildGroupAutoAddItems(
    { items: [{ channel_id: 9, model_name: 'm', priority: 4, weight: 1 }] },
    [
      { channel_id: 10, channel_name: 'c10', name: 'm', enabled: true },
      { channel_id: 11, channel_name: 'c11', name: 'm', enabled: true },
    ],
  ),
  [
    { channel_id: 10, model_name: 'm', priority: 5, weight: 1 },
    { channel_id: 11, model_name: 'm', priority: 6, weight: 1 },
  ],
);
```

- [ ] **Step 2: Run RED**

```bash
cd web
pnpm test
```

Expected: only the new helper tests fail because the module does not exist/behavior is not extracted yet.

- [ ] **Step 3: Implement the pure helper**

Do not import React, hooks, Query Client, or mutation code into `auto-add.ts`.

- [ ] **Step 4: Run GREEN**

```bash
pnpm test
```

- [ ] **Step 5: Commit**

```bash
git add web/src/components/modules/group/auto-add.ts web/tests/group-auto-add.test.mjs
git commit -m "refactor(group): extract auto add matching"
```

---

### Task 2: Make the existing Editor use the same helper

**Files:**
- Modify: `web/src/components/modules/group/Editor.tsx`
- Extend: `web/tests/group-auto-add.test.mjs`

**Interfaces:** no user-visible behavior change.

- [ ] **Step 1: Add a RED source contract**

The test should assert `Editor.tsx` imports and calls `resolveGroupAutoAddCandidates` and no longer contains its own `new RegExp(group.match_regex)`/parallel candidate filter block.

- [ ] **Step 2: Replace inline matching with the helper**

Editor may still own `useModelChannelList()` because it is a single modal/editor instance, not N card observers. Its local Auto Add button remains and continues to add matches to local `selectedMembers` only until Save.

- [ ] **Step 3: Preserve current user-facing behavior**

- button disabled/no-op when no new candidates;
- duplicates excluded;
- invalid regex surfaced using the existing validation surface;
- selected member default weight unchanged.

- [ ] **Step 4: Run tests/lint**

```bash
pnpm test
pnpm lint
```

- [ ] **Step 5: Commit**

```bash
git add web/src/components/modules/group/Editor.tsx web/tests/group-auto-add.test.mjs
git commit -m "refactor(group): reuse auto add resolver in editor"
```

---

### Task 3: Derive card Auto Add resolutions from the page-level shared snapshot

**Files:**
- Modify: `web/src/components/modules/group/index.tsx`
- Modify: `web/src/components/modules/group/Card.tsx`
- Extend: `web/tests/group-auto-add.test.mjs`

**Dependency:** uses the page-level `modelChannels` query already hoisted by the Group UI stability plan. Do **not** add `useModelChannelList()` to every `GroupCard` or `AutoAddPopover`.

**Interfaces:**

Extend `GroupCardProps`:

```ts
export type GroupCardProps = {
  // existing props from the stability slice...
  autoAddResolution: GroupAutoAddResolution;
};
```

At the page boundary, memoize all group resolutions once per group/model snapshot:

```ts
const autoAddByGroupId = useMemo(() => {
  const next = new Map<number, GroupAutoAddResolution>();
  for (const group of groups ?? []) {
    if (group.id == null) continue;
    next.set(group.id, resolveGroupAutoAddCandidates(group, modelChannels));
  }
  return next;
}, [groups, modelChannels]);
```

Use a shared empty resolution constant for missing IDs so memoized cards are not invalidated by a newly allocated fallback object.

- [ ] **Step 1: Add RED source contracts**

Assert:

- `index.tsx` computes Auto Add resolutions from its shared `modelChannels` snapshot;
- `Card.tsx` receives a resolution prop;
- neither `Card.tsx` nor `AutoAddPopover.tsx` calls `useModelChannelList()`.

- [ ] **Step 2: Implement page-side resolution map**

Do not recompute candidate matching inside the card body on every unrelated card mutation.

- [ ] **Step 3: Run tests/lint**

```bash
pnpm test
pnpm lint
```

- [ ] **Step 4: Commit**

```bash
git add web/src/components/modules/group/index.tsx web/src/components/modules/group/Card.tsx web/tests/group-auto-add.test.mjs
git commit -m "perf(group): prepare card auto add candidates"
```

---

### Task 4: Add the card-header Auto Add preview/confirm action

**Files:**
- Create: `web/src/components/modules/group/AutoAddPopover.tsx`
- Modify: `web/src/components/modules/group/Card.tsx`
- Extend: `web/tests/group-auto-add.test.mjs`

**Placement:** in the same compact action cluster as Copy, Preset, and Protocol Policy. Do not place it inside the large group body as a second primary button.

**Interfaces:**

```ts
export type AutoAddPopoverProps = {
  group: Group;
  resolution: GroupAutoAddResolution;
};
```

The component owns `useUpdateGroup()` only; it does not fetch models.

### Popover states

**Ready**

Show:

- title: Auto Add / 自动添加;
- match basis: Group Name / Regex;
- `N` new channel-model pairs;
- a bounded preview list (e.g. first 8), with `+N more` for larger sets;
- Confirm action.

**No matches**

Show an informational empty state. No mutation call.

**Invalid regex**

Show the regex error and direct the user to edit the group matching rule. Confirm disabled.

**Mutation pending**

Disable confirm and prevent double submission.

**Mutation success**

Close popover; the existing `useUpdateGroup()` optimistic/cache-invalidation path updates the card.

### Mutation payload

```ts
const items = buildGroupAutoAddItems(group, resolution.candidates);
updateGroup.mutate({
  id: group.id!,
  items_to_add: items,
});
```

No silent mode/name/regex update is allowed in this request.

- [ ] **Step 1: Add RED source contracts**

Assert `Card.tsx` places `<AutoAddPopover ... />` adjacent to the existing header tools and `AutoAddPopover.tsx` calls `useUpdateGroup()` with `items_to_add` only.

- [ ] **Step 2: Implement preview UI**

Use existing Popover/button design primitives. Preserve current header density; the trigger should be icon-sized with tooltip/accessibility label.

- [ ] **Step 3: Implement confirm mutation**

Use the helper-generated payload. Rely on the existing backend unique `(group_id, channel_id, model_name)` conflict-safe path as a final race-condition guard, not as the primary UI de-dupe mechanism.

- [ ] **Step 4: Run tests/lint**

```bash
pnpm test
pnpm lint
```

- [ ] **Step 5: Commit**

```bash
git add web/src/components/modules/group/AutoAddPopover.tsx web/src/components/modules/group/Card.tsx web/tests/group-auto-add.test.mjs
git commit -m "feat(group): add card auto add shortcut"
```

---

### Task 5: Add three-locale copy without enlarging the main message files

**Files:**
- Create: `web/src/provider/group-auto-add-messages.ts`
- Modify: `web/src/provider/locale.tsx`
- Extend: `web/tests/group-auto-add.test.mjs`

Follow the feature-catalog pattern already used for Channel Create.

**Interfaces:**

```ts
export const groupAutoAddMessages = {
  en: { groupAutoAdd: { /* ... */ } },
  zh_hans: { groupAutoAdd: { /* ... */ } },
  zh_hant: { groupAutoAdd: { /* ... */ } },
} as const;
```

Minimum keys:

```text
trigger
previewTitle
basisLabel
basisName
basisRegex
newMatches
noMatches
invalidRegex
moreMatches
confirm
cancel
pending
```

- [ ] **Step 1: Add RED locale tests**

Assert all three locale branches contain exactly the required keys and the component uses `useTranslations('groupAutoAdd')` rather than hard-coded visible copy.

- [ ] **Step 2: Implement and merge messages in `locale.tsx`**

Do not duplicate the large base catalogs. Merge this small feature catalog after the normal locale messages, matching the Channel Create implementation pattern.

- [ ] **Step 3: Run tests/lint**

```bash
pnpm test
pnpm lint
```

- [ ] **Step 4: Commit**

```bash
git add web/src/provider/group-auto-add-messages.ts web/src/provider/locale.tsx web/tests/group-auto-add.test.mjs web/src/components/modules/group/AutoAddPopover.tsx
git commit -m "feat(group): localize auto add shortcut"
```

---

### Task 6: Full verification and interaction gate

- [ ] **Step 1: Run full repository CI**

Required:

```text
governance: success
backend Vet/full tests: success
frontend lint/tests/build: success
```

- [ ] **Step 2: Verify parity with editor Auto Add**

For the same group and model snapshot, compare the editor-local Auto Add candidate set with the card-header preview. Pass condition: identical `(channel_id, model_name)` set and stable ordering.

- [ ] **Step 3: Verify name matching**

Group without regex: preview matches exactly what the existing name matcher would add. Existing members are excluded.

- [ ] **Step 4: Verify regex matching**

Valid regex: correct candidates. Invalid regex: no request sent, clear error displayed, existing group unchanged.

- [ ] **Step 5: Verify race/duplicate safety**

Open two browser tabs on the same group and confirm the same Auto Add operation in both. Pass condition: backend conflict-safe insertion prevents duplicate group items; UI converges after query refresh.

- [ ] **Step 6: Verify large candidate preview**

For a group matching many channels, preview remains bounded and the confirm request contains every de-duplicated candidate exactly once.

- [ ] **Step 7: Scope gate**

Confirm changed production code is limited to Group frontend and feature-scoped locale integration. No Go production change, DB migration, package dependency, routing change, or channel/global Auto Group behavior change.
