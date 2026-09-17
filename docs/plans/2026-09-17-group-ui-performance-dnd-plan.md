# Group UI Performance + DnD Correctness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate Group-page drag-position jumps and materially reduce scrolling flicker/jank without replacing the existing DnD library or redesigning Group behavior.

**Architecture:** Reparent the active draggable through `@hello-pangea/dnd`'s first-class clone API so drag coordinates are independent of virtualized/animated ancestors. Then give Group cards a deterministic outer height, allow the Group virtual grid to skip dynamic row measurement, and opt Group into compositor-friendly transform positioning. Lift the shared model-channel query/index above individual cards so periodic refreshes do not create one observer/index rebuild per card.

**Tech Stack:** React 19, Next.js 16, `@hello-pangea/dnd@18`, `@tanstack/react-virtual@3`, TanStack Query, existing Node source-contract tests.

**Spec:** [`2026-09-17-group-ux-autoadd-health-audit.md`](./2026-09-17-group-ux-autoadd-health-audit.md)

## Global Constraints

- Baseline: `main@6978f0cc02770b5ca94fafd85914130471f40442`.
- No new frontend dependency.
- Do not migrate to another DnD library in this slice.
- Do not virtualize the inner Group member list in this slice.
- Preserve current member reorder / delete / weight semantics and optimistic Group update behavior.
- Preserve `VirtualizedGrid` defaults for non-Group consumers; Group-specific performance options must be opt-in.
- No API, Go backend, DB migration, routing behavior, or deployment change.
- No local Mac build/deploy; repository CI is the verification boundary.
- Use `node --test tests/*.test.mjs` for frontend test execution through the existing harness.

---

## File Structure

**Create**

- `web/src/components/modules/group/layout.ts`
  - Group-only fixed layout constants.
- `web/tests/group-dnd-virtualization.test.mjs`
  - Source contracts for drag clone isolation and Group-specific virtual-grid settings.
- `web/tests/group-card-data-flow.test.mjs`
  - Source contract proving the model-channel query is owned by `Group()` rather than every `GroupCard`.

**Modify**

- `web/src/components/modules/group/ItemList.tsx`
  - Shared member-row renderer + official DnD clone/reparenting path.
- `web/src/components/common/VirtualizedGrid.tsx`
  - Opt-in transform positioning and opt-out dynamic measurement.
- `web/src/components/modules/group/index.tsx`
  - Fixed-height Group virtualization and one shared model-channel query/index.
- `web/src/components/modules/group/Card.tsx`
  - Deterministic card height; consume shared model-channel index; flex member viewport.

The plan intentionally does not edit `web/src/components/ui/dialog.tsx`. Once the draggable is correctly reparented, the dialog no longer needs new drag-specific exceptions in this slice; removing old dialog compatibility comments can be considered only after regression evidence proves no other DnD consumer depends on them.

---

### Task 1: Lock the drag-clone and fixed-layout contracts with RED tests

**Files:**
- Create: `web/tests/group-dnd-virtualization.test.mjs`
- Create: `web/tests/group-card-data-flow.test.mjs`

**Interfaces:**
- Produces the behavioral/source contracts all later tasks must satisfy.

- [ ] **Step 1: Add a RED contract for official DnD cloning**

Create `web/tests/group-dnd-virtualization.test.mjs` with a source helper matching the repository's existing UI contract-test style. Assert all of the following:

```js
const itemList = await source('../src/components/modules/group/ItemList.tsx');
assert.match(itemList, /renderClone=/);
assert.match(itemList, /getContainerForClone=/);
assert.match(itemList, /document\.body/);
```

Also assert that the Group grid imports its fixed layout constant and opts into the new virtual-grid mode:

```js
const groupPage = await source('../src/components/modules/group/index.tsx');
assert.match(groupPage, /GROUP_CARD_HEIGHT/);
assert.match(groupPage, /measureRows=\{false\}/);
assert.match(groupPage, /positionMode="transform"/);
```

- [ ] **Step 2: Add a RED contract for shared model-channel ownership**

Create `web/tests/group-card-data-flow.test.mjs` and assert:

```js
const groupPage = await source('../src/components/modules/group/index.tsx');
const card = await source('../src/components/modules/group/Card.tsx');

assert.match(groupPage, /useModelChannelList\(\)/);
assert.doesNotMatch(card, /useModelChannelList\(\)/);
assert.match(card, /modelChannelByKey/);
```

- [ ] **Step 3: Run the focused tests and verify RED**

Run in repository CI/authorized execution environment:

```bash
cd web
node --test tests/group-dnd-virtualization.test.mjs tests/group-card-data-flow.test.mjs
```

Expected: FAIL because `renderClone`, Group fixed-layout options, and lifted query ownership do not yet exist.

- [ ] **Step 4: Commit the RED tests**

```bash
git add web/tests/group-dnd-virtualization.test.mjs web/tests/group-card-data-flow.test.mjs
git commit -m "test(group): lock drag and virtualization contracts"
```

---

### Task 2: Reparent the active member drag with the supported clone API

**Files:**
- Modify: `web/src/components/modules/group/ItemList.tsx`
- Test: `web/tests/group-dnd-virtualization.test.mjs`

**Interfaces:**
- Produces: the active drag visual is rendered outside the virtualized/animated Group DOM tree.
- Preserves: `MemberListProps`, `onReorder`, `onDrop`, `onDragStart`, `onDragFinish` and current member IDs.

- [ ] **Step 1: Extract one visual member-row renderer**

Refactor the visible row so normal `Draggable` rendering and `renderClone` use the same size/structure. A concrete interface should be equivalent to:

```ts
type MemberRowDndProps = {
    innerRef: DraggableProvided['innerRef'];
    draggableProps: DraggableProvided['draggableProps'];
    dragHandleProps: DraggableProvided['dragHandleProps'];
    isDragging: boolean;
};

type MemberItemProps = {
    member: SelectedMember;
    index: number;
    showWeight: boolean;
    showConfirmDelete: boolean;
    isRemoving: boolean;
    layoutScope: string;
    dnd: MemberRowDndProps;
    clone?: boolean;
    onRemove: (id: string) => void;
    onWeightChange?: (id: string, weight: number) => void;
};
```

`clone` must render the same outer dimensions as the ordinary row. Clone-only controls must not mutate state; use no-op action callbacks or disable pointer interaction while keeping the action slot dimensions stable.

- [ ] **Step 2: Add `renderClone` to the existing `Droppable`**

Implement the clone from `rubric.source.index` rather than duplicating member lookup logic:

```tsx
<Droppable
    droppableId={`members-${layoutScope}`}
    getContainerForClone={() => document.body}
    renderClone={(provided, snapshot, rubric) => {
        const member = members[rubric.source.index];
        if (!member) return null;
        return (
            <MemberItem
                member={member}
                index={rubric.source.index}
                onRemove={() => undefined}
                onWeightChange={() => undefined}
                isRemoving={false}
                showWeight={showWeight}
                showConfirmDelete={false}
                layoutScope={`${layoutScope}-clone`}
                clone
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

Use the library clone API directly; do not add a hand-rolled `ReactDOM.createPortal`.

- [ ] **Step 3: Keep dimensions stable during drag**

During an active drag:

- do not change member row padding/height;
- do not remove the placeholder;
- do not animate grid-row collapse for the active member;
- keep the existing `onDragFinish` cleanup in the `finally` block.

The drag shadow/z-index can remain visual-only.

- [ ] **Step 4: Run focused tests**

```bash
cd web
node --test tests/group-dnd-virtualization.test.mjs
```

Expected: the clone assertions pass; the later fixed-layout assertions remain RED until Task 3/4.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/modules/group/ItemList.tsx web/tests/group-dnd-virtualization.test.mjs
git commit -m "fix(group): isolate member drag clone"
```

---

### Task 3: Add Group-safe fixed-row options to `VirtualizedGrid`

**Files:**
- Modify: `web/src/components/common/VirtualizedGrid.tsx`
- Test: `web/tests/group-dnd-virtualization.test.mjs`

**Interfaces:**
- Add props:

```ts
measureRows?: boolean; // default true
positionMode?: 'top' | 'transform'; // default 'top' for backward compatibility
```

- [ ] **Step 1: Extend the public component props without changing defaults**

Add:

```ts
measureRows = true,
positionMode = 'top',
```

Non-Group consumers must retain current dynamic measurement / `top` behavior unless they explicitly opt in.

- [ ] **Step 2: Make measurement optional**

Only assign the row measurement ref when enabled:

```tsx
ref={measureRows ? rowVirtualizer.measureElement : undefined}
```

Apply this consistently to header/item/footer virtual rows if they use the shared positioning path. If a consumer provides a dynamic header/footer and needs measurement, it must keep `measureRows=true`; Group will not rely on a virtual header/footer.

- [ ] **Step 3: Add the transform positioning mode**

Use one helper to prevent header/item/footer divergence:

```ts
function virtualRowStyle(start: number, mode: 'top' | 'transform'): React.CSSProperties {
    if (mode === 'transform') {
        return { top: 0, transform: `translateY(${start}px)` };
    }
    return { top: `${start}px` };
}
```

The Group page will opt into transform mode only after Task 2's body clone removes the fixed-position draggable from this ancestor chain.

- [ ] **Step 4: Run existing frontend tests plus focused contract**

```bash
cd web
node --test tests/*.test.mjs
```

Expected: existing behavior remains GREEN; Group fixed-layout assertions may still be RED until Task 4.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/common/VirtualizedGrid.tsx web/tests/group-dnd-virtualization.test.mjs
git commit -m "perf(web): add fixed virtual row mode"
```

---

### Task 4: Give Group cards a deterministic virtual footprint

**Files:**
- Create: `web/src/components/modules/group/layout.ts`
- Modify: `web/src/components/modules/group/Card.tsx`
- Modify: `web/src/components/modules/group/index.tsx`
- Test: `web/tests/group-dnd-virtualization.test.mjs`

**Interfaces:**
- Produces:

```ts
export const GROUP_CARD_HEIGHT = 544;
export const GROUP_GRID_GAP = 16;
```

The exact constants are shared by card CSS and virtualizer estimate so they cannot drift independently.

- [ ] **Step 1: Add Group layout constants**

Create `layout.ts`:

```ts
export const GROUP_CARD_HEIGHT = 544;
export const GROUP_GRID_GAP = 16;
```

- [ ] **Step 2: Make the outer card fixed-height and the member area flexible**

Change the outer `article` to use the shared height:

```tsx
<article
    style={{ height: GROUP_CARD_HEIGHT }}
    className="relative group/card flex min-h-0 flex-col ..."
>
```

Replace the current member section `h-96` with:

```tsx
<section className="relative min-h-0 flex-1 overflow-hidden rounded-lg border ...">
```

Do not hide the header/mode/health content. The member viewport absorbs the remaining height.

- [ ] **Step 3: Opt Group into fixed transform virtualization**

In `group/index.tsx`:

```tsx
<VirtualizedGrid
    items={visibleGroups}
    columns={resolveGroupColumns}
    estimateItemHeight={GROUP_CARD_HEIGHT}
    gap={GROUP_GRID_GAP}
    measureRows={false}
    positionMode="transform"
    ...
/>
```

- [ ] **Step 4: Verify the source contract GREEN**

```bash
cd web
node --test tests/group-dnd-virtualization.test.mjs
```

Expected: all DnD/fixed-layout assertions pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/modules/group/layout.ts web/src/components/modules/group/Card.tsx web/src/components/modules/group/index.tsx web/tests/group-dnd-virtualization.test.mjs
git commit -m "perf(group): stabilize virtual card layout"
```

---

### Task 5: Lift model-channel subscription/indexing out of every card

**Files:**
- Modify: `web/src/components/modules/group/index.tsx`
- Modify: `web/src/components/modules/group/Card.tsx`
- Test: `web/tests/group-card-data-flow.test.mjs`

**Interfaces:**
- `GroupCard` receives:

```ts
interface GroupCardProps {
    group: Group;
    modelChannelByKey: ReadonlyMap<string, LLMChannel>;
}
```

- [ ] **Step 1: Subscribe once in the Group page**

In `Group()`:

```ts
const { data: modelChannels = [] } = useModelChannelList();
const modelChannelByKey = useMemo(() => {
    const map = new Map<string, LLMChannel>();
    for (const mc of modelChannels) {
        map.set(modelChannelKey(mc.channel_id, mc.name), mc);
    }
    return map;
}, [modelChannels]);
```

- [ ] **Step 2: Pass the shared index to each card**

```tsx
renderItem={(group) => (
    <GroupCard group={group} modelChannelByKey={modelChannelByKey} />
)}
```

- [ ] **Step 3: Remove the per-card query/map**

Delete `useModelChannelList()` and the duplicate `modelChannelByKey` `useMemo` from `Card.tsx`. Preserve `displayMembers` mapping behavior exactly.

Do not move Group Health data in this task; health-query ownership will be handled with the health semantic slice to avoid touching the same component twice for different reasons.

- [ ] **Step 4: Run the source contract**

```bash
cd web
node --test tests/group-card-data-flow.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/modules/group/index.tsx web/src/components/modules/group/Card.tsx web/tests/group-card-data-flow.test.mjs
git commit -m "perf(group): share model channel index"
```

---

### Task 6: Full frontend regression and interaction gate

**Files:**
- No new production file required unless a failing regression identifies one.

- [ ] **Step 1: Run frontend tests**

```bash
cd web
node --test tests/*.test.mjs
```

Expected: PASS.

- [ ] **Step 2: Run lint and production build through the repository CI contract**

Expected CI frontend steps:

```text
lint: PASS
test: PASS
build: PASS
```

Do not install/build locally on the user's Mac.

- [ ] **Step 3: Perform browser interaction acceptance on an authorized runtime**

Verify all of the following before closing the slice:

```text
[ ] Repeated fast up/down scrolling on Group page does not visibly flash blank/repositioned rows.
[ ] Leave Group page and return; first scroll does not trigger repeated row-height snapping.
[ ] Drag first, middle, and last visible member; the active block remains under the pointer.
[ ] Drag after scrolling the member viewport away from its top; no jump toward viewport center.
[ ] Drag inside cards in 1-, 2-, 3-, and 4-column layouts.
[ ] Drop at same index and cancel outside destination; lifecycle clears correctly.
[ ] Reorder persists once without a temporary old-order flash.
[ ] Delete confirmation and weight editing still work.
[ ] Edit dialog member reorder still works with the same clone path.
```

- [ ] **Step 4: Capture before/after performance evidence**

Using browser Performance/React profiling on the same representative dataset, record at minimum:

```text
- number of GroupCard commits during one 30-second model-channel refresh;
- long-task count during a fixed rapid-scroll interaction;
- whether virtual rows are being re-measured during ordinary Group scrolling;
```

The acceptance requirement is qualitative correctness plus a reduction in redundant GroupCard commits; do not invent a numeric FPS threshold if the baseline was not measured before implementation.

- [ ] **Step 5: Final full repository CI gate**

Required:

```text
governance: PASS
backend Vet/full Go tests: PASS (unchanged backend still regressed)
frontend lint/test/build: PASS
```

- [ ] **Step 6: Commit evidence-only plan status update only after code CI is green**

Update this plan with the actual branch head / CI run / acceptance results. Do not mark the slice complete before those artifacts exist.

---

## Explicit non-goals / follow-up gate

Do not implement inner member-list virtualization in this slice. If a Group containing a large member set is still slow after the above corrections, start a fresh source audit for **virtual DnD member lists** and use the `@hello-pangea/dnd` virtual-list clone contract from the beginning.

Do not remove global animation systems, Radix UI, or `motion/react` wholesale. Only a profiler-backed remaining hotspot justifies a separate animation simplification slice.