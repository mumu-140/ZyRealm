# Group UI Stability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** make the Group page scroll smoothly and make member reordering track the pointer without jumping, while preserving all group semantics and existing APIs.

**Architecture:** remove page-wide query/index work from individual cards, isolate drag rendering from the virtual/card coordinate tree with the DnD cloning API, lock the outer group scroll surface during an active member drag, and use a plain grid for ordinary group counts with virtualization only for large lists. Keep the existing `@hello-pangea/dnd` and TanStack virtualizer; no library migration in this slice.

**Tech Stack:** Next.js 16, React 19, TanStack Query, TanStack React Virtual 3.13.19, `@hello-pangea/dnd` 18.0.1, next-intl, Node test runner.

**Spec:** `docs/plans/2026-09-17-group-ui-auto-add-health-audit.md`

## Global Constraints

- Baseline implementation branch must start from fresh `main`; audited baseline was `6978f0cc02770b5ca94fafd85914130471f40442`.
- No local Mac build, package install, or deployment.
- No backend API, DB migration, relay selection, or group data-model changes.
- Do not replace `@hello-pangea/dnd` in this plan.
- Do not solve drag offset with another transform/top/z-index-only patch.
- Preserve inline card reorder and editor reorder behavior.
- Keep group/model/health polling semantics unless a later measured change is separately approved.
- Use TDD; frontend verification is `pnpm lint && pnpm test && pnpm build` in CI/approved remote environment.

---

## File Structure

**Create**
- `web/src/components/modules/group/GroupGrid.tsx` — Group-specific plain/virtualized rendering boundary and outer-scroll lock.
- `web/src/components/modules/group/groupViewData.ts` — pure index/member projection helpers so one page-wide model/health snapshot can feed all cards.
- `web/tests/group-ui-stability.test.mjs` — pure/source-contract regression coverage for the page architecture and DnD clone path.

**Modify**
- `web/src/components/modules/group/index.tsx` — own shared group/model/health queries and build indexes once.
- `web/src/components/modules/group/Card.tsx` — consume prepared shared data; separate reorder optimistic state from unrelated mutation pending state; memoize card boundary.
- `web/src/components/modules/group/health.tsx` — accept a health view prop instead of subscribing to the complete list per card.
- `web/src/components/modules/group/ItemList.tsx` — use `renderClone`/body reparenting and expose active-drag lifecycle.
- `web/src/components/common/VirtualizedGrid.tsx` — add an opt-in `scrollLocked` prop only; generic behavior otherwise unchanged.

---

### Task 1: Lock the architectural regressions with RED tests

**Files:**
- Create: `web/tests/group-ui-stability.test.mjs`

**Interfaces:**
- Consumes current source files only.
- Produces regression contracts used by Tasks 2–5.

- [ ] **Step 1: Write failing source-contract tests**

Add tests that read the relevant source files and assert the intended architecture. The initial test must fail on current `main` for all new contracts.

```js
import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';

const read = (path) => fs.readFileSync(new URL(path, import.meta.url), 'utf8');

const groupIndex = read('../src/components/modules/group/index.tsx');
const card = read('../src/components/modules/group/Card.tsx');
const health = read('../src/components/modules/group/health.tsx');
const itemList = read('../src/components/modules/group/ItemList.tsx');

// One page-wide observer, not N card observers.
test('group page owns shared model and health queries', () => {
  assert.match(groupIndex, /useModelChannelList\(/);
  assert.match(groupIndex, /useGroupHealthList\(/);
  assert.doesNotMatch(card, /useModelChannelList\(/);
  assert.doesNotMatch(health, /useGroupHealthList\(/);
});

test('member drag uses a body-level clone', () => {
  assert.match(itemList, /renderClone=/);
  assert.match(itemList, /getContainerForClone=/);
  assert.match(itemList, /document\.body/);
});

test('group page has a plain-grid path before virtualization', () => {
  const grid = read('../src/components/modules/group/GroupGrid.tsx');
  assert.match(grid, /GROUP_VIRTUALIZE_AFTER\s*=\s*32/);
  assert.match(grid, /items\.length\s*<=\s*GROUP_VIRTUALIZE_AFTER/);
});
```

- [ ] **Step 2: Run the focused test and capture RED evidence**

Run from `web/`:

```bash
pnpm test -- group-ui-stability.test.mjs
```

If the repository script still expands all `tests/*.test.mjs`, accept a full test run. Expected result: existing tests stay green and the new contracts fail because `GroupGrid.tsx` does not exist, `GroupCard` still owns `useModelChannelList()`, `GroupHealthBadge` still owns `useGroupHealthList()`, and DnD clone props are absent.

- [ ] **Step 3: Commit RED only**

```bash
git add web/tests/group-ui-stability.test.mjs
git commit -m "test(group): lock UI stability architecture"
```

---

### Task 2: Hoist shared model/health data and eliminate repeated per-card indexing

**Files:**
- Create: `web/src/components/modules/group/groupViewData.ts`
- Modify: `web/src/components/modules/group/index.tsx`
- Modify: `web/src/components/modules/group/Card.tsx`
- Modify: `web/src/components/modules/group/health.tsx`
- Extend test: `web/tests/group-ui-stability.test.mjs`

**Interfaces:**

Produce these pure helpers:

```ts
import type { LLMChannel } from '@/api/endpoints/model';
import type { Group, GroupHealthGroupView } from '@/api/endpoints/group';
import type { SelectedMember } from './ItemList';

export type ModelChannelIndex = ReadonlyMap<string, LLMChannel>;
export type GroupHealthIndex = ReadonlyMap<number, GroupHealthGroupView>;

export function buildModelChannelIndex(modelChannels: readonly LLMChannel[]): ModelChannelIndex;
export function buildGroupHealthIndex(views: readonly GroupHealthGroupView[]): GroupHealthIndex;
export function projectGroupMembers(group: Group, index: ModelChannelIndex): SelectedMember[];
```

`GroupCard` becomes a presentational/query-local mutation component:

```ts
export type GroupCardProps = {
  group: Group;
  displayMembers: SelectedMember[];
  healthView?: GroupHealthGroupView | null;
  onMemberDragStateChange?: (dragging: boolean) => void;
};
```

`GroupHealthBadge` becomes:

```ts
export function GroupHealthBadge({
  groupId,
  view,
}: {
  groupId?: number;
  view?: GroupHealthGroupView | null;
})
```

It may still own `useRunGroupHealth()` because that mutation is card-local, but it must not call `useGroupHealthList()`.

- [ ] **Step 1: Extend RED tests for pure indexes**

Add direct imports from `groupViewData.ts` and assert one input model snapshot resolves multiple groups without rebuilding independent maps.

```js
import {
  buildModelChannelIndex,
  buildGroupHealthIndex,
  projectGroupMembers,
} from '../src/components/modules/group/groupViewData.ts';

test('shared indexes project card data deterministically', () => {
  const modelIndex = buildModelChannelIndex([
    { name: 'm1', channel_id: 7, channel_name: 'c7', enabled: true },
    { name: 'm1', channel_id: 8, channel_name: 'c8', enabled: false },
  ]);
  const members = projectGroupMembers({
    id: 1,
    name: 'm1',
    mode: 1,
    match_regex: '',
    items: [{ id: 11, channel_id: 8, model_name: 'm1', priority: 1, weight: 2 }],
  }, modelIndex);
  assert.equal(members[0].channel_name, 'c8');
  assert.equal(members[0].enabled, false);
  assert.equal(members[0].weight, 2);

  const healthIndex = buildGroupHealthIndex([{ group_id: 1, group_name: 'm1', group_mode: 1 }]);
  assert.equal(healthIndex.get(1)?.group_name, 'm1');
});
```

- [ ] **Step 2: Implement the pure indexes**

`buildModelChannelIndex()` must key entries using the existing `modelChannelKey(channel_id, name)` helper. `projectGroupMembers()` must preserve the current priority sort, `item_id`, weight, disabled-state fallback, and `Channel ${id}` name fallback exactly.

- [ ] **Step 3: Move shared queries to `Group()`**

In `index.tsx`, call exactly once:

```ts
const { data: groups } = useGroupList();
const { data: modelChannels = [] } = useModelChannelList();
const { data: healthViews = [] } = useGroupHealthList();

const modelChannelIndex = useMemo(
  () => buildModelChannelIndex(modelChannels),
  [modelChannels],
);
const healthIndex = useMemo(
  () => buildGroupHealthIndex(healthViews),
  [healthViews],
);
```

Project `displayMembers` at the page boundary and pass `healthIndex.get(group.id)` into the card.

- [ ] **Step 4: Remove card/badge broad subscriptions**

Delete `useModelChannelList()` from `GroupCard` and `useGroupHealthList()` from `GroupHealthBadge`. Do not change query intervals in this task.

- [ ] **Step 5: Memoize the card boundary**

Wrap the exported card with `memo` and ensure the page passes stable `displayMembers` references for unchanged groups. Do not add a custom equality function that ignores real group mutations.

- [ ] **Step 6: Run tests and frontend lint**

```bash
pnpm test
pnpm lint
```

Expected: all current tests plus the shared-data tests pass.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/modules/group/groupViewData.ts web/src/components/modules/group/index.tsx web/src/components/modules/group/Card.tsx web/src/components/modules/group/health.tsx web/tests/group-ui-stability.test.mjs
git commit -m "perf(group): hoist shared card data"
```

---

### Task 3: Fix the optimistic-member flicker path

**Files:**
- Modify: `web/src/components/modules/group/Card.tsx`
- Extend test: `web/tests/group-ui-stability.test.mjs`

**Problem to remove:** current `renderedMembers` switches to the `members` drag buffer whenever **any** `updateGroup.isPending` is true. The drag buffer is initialized only on drag start. An unrelated group mutation can therefore render stale/empty drag state.

**Interfaces:**

Replace generic `members`/`updateGroup.isPending` coupling with explicit reorder state:

```ts
const [dragMembers, setDragMembers] = useState<SelectedMember[]>([]);
const [pendingReorderMembers, setPendingReorderMembers] = useState<SelectedMember[] | null>(null);

const renderedMembers = isDragging
  ? dragMembers
  : pendingReorderMembers ?? effectiveDisplayMembers;
```

- [ ] **Step 1: Add a RED source contract**

```js
test('card does not swap member data for every pending mutation', () => {
  assert.doesNotMatch(card, /isDragging\s*\|\|\s*updateGroup\.isPending\s*\?\s*members/);
  assert.match(card, /pendingReorderMembers/);
});
```

- [ ] **Step 2: Implement explicit reorder optimistic state**

On drag start, copy `effectiveDisplayMembers` into `dragMembers`. During drag, `onReorder` updates only `dragMembers`. On a changed drop, set `pendingReorderMembers` to the dropped order before sending the mutation. Clear it on mutation success/error after the group query/cache has been invalidated according to the existing mutation contract.

Mode changes, weight changes, pinning, and delete confirmation must never switch the list to drag state.

- [ ] **Step 3: Run tests**

```bash
pnpm test
pnpm lint
```

- [ ] **Step 4: Commit**

```bash
git add web/src/components/modules/group/Card.tsx web/tests/group-ui-stability.test.mjs
git commit -m "fix(group): isolate reorder optimistic state"
```

---

### Task 4: Reparent the active DnD clone to `document.body`

**Files:**
- Modify: `web/src/components/modules/group/ItemList.tsx`
- Extend test: `web/tests/group-ui-stability.test.mjs`

**Interfaces:**

Add:

```ts
onDragStateChange?: (dragging: boolean) => void;
```

to `MemberListProps`.

Refactor `MemberItem` so both the normal draggable renderer and `renderClone` use the same component and styling. Add to `Droppable`:

```tsx
<Droppable
  droppableId={`members-${layoutScope}`}
  getContainerForClone={() => document.body}
  renderClone={(provided, snapshot, rubric) => {
    const member = members[rubric.source.index];
    return (
      <MemberItem
        member={member}
        index={rubric.source.index}
        {...sharedMemberProps}
        dnd={{
          innerRef: provided.innerRef,
          draggableProps: provided.draggableProps,
          dragHandleProps: provided.dragHandleProps,
          isDragging: snapshot.isDragging,
        }}
      />
    );
  }}
>
```

- [ ] **Step 1: Run the existing RED clone contract from Task 1**

Expected: fail before implementation.

- [ ] **Step 2: Add the clone path**

Do not manually calculate pointer offsets. Do not strip the style supplied by `@hello-pangea/dnd`.

- [ ] **Step 3: Wire drag lifecycle**

At drag start call both `onDragStart?.()` and `onDragStateChange?.(true)`. In the `finally` block of drag end call `onDragStateChange?.(false)` before/with `onDragFinish?.()` so cancellation cannot leave the page locked.

- [ ] **Step 4: Run tests/lint**

```bash
pnpm test
pnpm lint
```

- [ ] **Step 5: Commit**

```bash
git add web/src/components/modules/group/ItemList.tsx web/tests/group-ui-stability.test.mjs
git commit -m "fix(group): reparent member drag clone"
```

---

### Task 5: Add Group-specific adaptive grid and lock outer scrolling while dragging

**Files:**
- Create: `web/src/components/modules/group/GroupGrid.tsx`
- Modify: `web/src/components/modules/group/index.tsx`
- Modify: `web/src/components/modules/group/Card.tsx`
- Modify: `web/src/components/common/VirtualizedGrid.tsx`
- Extend test: `web/tests/group-ui-stability.test.mjs`

**Interfaces:**

`GroupGrid.tsx` exports:

```ts
export const GROUP_VIRTUALIZE_AFTER = 32;

export type GroupGridProps = {
  items: Group[];
  renderItem: (group: Group) => ReactNode;
  scrollLocked: boolean;
};
```

Behavior:

- if `items.length <= 32`, render a normal responsive grid inside one outer `h-full overflow-y-auto` surface;
- if `items.length > 32`, delegate to `VirtualizedGrid` with the current column resolver and `estimateItemHeight={520}`;
- both paths use the same outer-scroll lock semantics while dragging.

Extend generic `VirtualizedGridProps<T>` with:

```ts
scrollLocked?: boolean;
```

and only change its scroll class:

```tsx
className={cn(
  'relative h-full w-full overscroll-contain rounded-t-3xl',
  scrollLocked ? 'overflow-hidden' : 'overflow-y-auto',
)}
```

No other generic virtualizer behavior changes in this task.

- [ ] **Step 1: Add RED contracts for threshold and scroll lock**

```js
test('group grid uses a plain path up to 32 groups', () => {
  const grid = read('../src/components/modules/group/GroupGrid.tsx');
  assert.match(grid, /GROUP_VIRTUALIZE_AFTER\s*=\s*32/);
  assert.match(grid, /items\.length\s*<=\s*GROUP_VIRTUALIZE_AFTER/);
});

test('outer group scrolling can be locked during member drag', () => {
  const virtualGrid = read('../src/components/common/VirtualizedGrid.tsx');
  assert.match(virtualGrid, /scrollLocked\?/);
  assert.match(virtualGrid, /overflow-hidden/);
});
```

- [ ] **Step 2: Implement `GroupGrid`**

The plain path must preserve the current responsive 1/2/3/4-column breakpoints. It must not introduce per-card scrolling or additional query hooks.

- [ ] **Step 3: Coordinate drag state at `Group()`**

```ts
const [memberDragActive, setMemberDragActive] = useState(false);
```

Pass `setMemberDragActive` to each `GroupCard`, and pass `memberDragActive` to `GroupGrid`. Only the outer page/card-grid scroll is locked; the member list remains scrollable.

- [ ] **Step 4: Keep virtual rows mounted during active drag by preventing outer scroll**

No scrolling means the active virtual row cannot be scrolled out and unmounted during the drag. Do not change `overscan` in this task.

- [ ] **Step 5: Run frontend suite**

```bash
pnpm lint
pnpm test
pnpm build
```

- [ ] **Step 6: Commit**

```bash
git add web/src/components/modules/group/GroupGrid.tsx web/src/components/modules/group/index.tsx web/src/components/modules/group/Card.tsx web/src/components/common/VirtualizedGrid.tsx web/tests/group-ui-stability.test.mjs
git commit -m "perf(group): stabilize grid scrolling and drag"
```

---

### Task 6: Remote/browser acceptance gate

**Files:** none unless a defect is discovered; any corrective change returns to RED/GREEN before this gate is repeated.

- [ ] **Step 1: Run full repository CI**

Required jobs:

```text
governance: success
backend Vet: success
backend full tests: success
frontend lint: success
frontend tests: success
frontend build: success
```

- [ ] **Step 2: Verify ordinary-size group page (`<=32` groups)**

On the deployed/approved remote test environment, repeat each action ten times:

1. enter Group page;
2. scroll from top to bottom continuously and back;
3. leave the page and immediately re-enter;
4. wait through at least one 30-second group/model/health refetch boundary.

Pass condition: no blank-card flash, no card disappearance/reappearance pulse, and no visible whole-page redraw at polling boundaries.

- [ ] **Step 3: Verify drag at three viewport positions**

For a group near top, middle, and bottom of viewport:

1. drag first member down at least three positions;
2. drag a middle member upward;
3. cancel a drag outside destination;
4. repeat while the member list itself is scrolled.

Pass condition: dragged clone remains under pointer; it never jumps to the viewport center; no outer Group page scrolling occurs during active drag; reorder persists after mutation refresh.

- [ ] **Step 4: Verify unrelated mutations do not blank member lists**

Exercise mode switch, weight edit, pin/unpin, and delete confirmation while observing the member list. Pass condition: only the targeted control changes; members do not temporarily switch to an empty/stale drag buffer.

- [ ] **Step 5: Verify large-list path**

With more than 32 groups in the remote fixture, confirm virtualization still limits mounted group rows and that the same drag checks pass for currently visible groups.

- [ ] **Step 6: Scope gate before Ready/Merge**

Changed production files must be limited to group UI and the opt-in `VirtualizedGrid.scrollLocked` extension. Confirm:

- no Go production changes;
- no DB migration;
- no package dependency changes;
- no deployment files;
- no routing/balancer changes.
