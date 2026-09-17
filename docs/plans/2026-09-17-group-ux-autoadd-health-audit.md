# Group UX / Auto-Add / Health Probe Source Audit

> **Status:** SOURCE-AUDITED / PLAN ONLY. No production code, database schema, deployment, or runtime behavior is changed by this document.
>
> **Baseline:** `main@6978f0cc02770b5ca94fafd85914130471f40442`.

## Goal

Investigate five reported Group-page problems before implementation:

1. Group page is noticeably laggy.
2. Dragging a channel/model member is not cursor-locked; the dragged block can jump toward the middle of the screen.
3. Vertical scrolling visibly flickers, including after leaving and re-entering the Group page.
4. Improve Auto Add and expose a one-click action on each Group card next to the existing Copy / Preset / Protocol / Edit actions.
5. Re-evaluate Group Health / Full Probe semantics because some providers reject synthetic probe traffic, and determine whether active probes participate in actual routing health.

The work is intentionally split into three implementation slices because these are different failure domains:

- **Slice A:** Group rendering / virtualization / DnD performance.
- **Slice B:** Group Auto Add quick action.
- **Slice C:** Active diagnostic probe vs real-traffic health semantics.

No implementation slice should absorb the other two merely because they share the Group screen.

---

## A. Group page performance and drag audit

### Current rendering topology

The Group page currently nests two independent scrolling / positioning systems:

```text
Group page
└── VirtualizedGrid (@tanstack/react-virtual)
    └── absolute virtual row
        └── GroupCard
            └── member viewport (overflow-y-auto)
                └── @hello-pangea/dnd DragDropContext
                    └── Droppable
                        └── Draggable member rows
```

Relevant files:

- `web/src/components/modules/group/index.tsx`
- `web/src/components/common/VirtualizedGrid.tsx`
- `web/src/components/modules/group/Card.tsx`
- `web/src/components/modules/group/ItemList.tsx`
- `web/src/components/ui/dialog.tsx`

### Drag positioning already has two workaround patches

`@hello-pangea/dnd` moves the active draggable with `position: fixed`. Its official reparenting guide explicitly warns that an ancestor `transform` changes the fixed-position containing block and causes incorrect drag positioning. The library's first-class solution is `Droppable.renderClone` / `getContainerForClone`, typically reparenting the active clone to `document.body`.

Reference:

- https://github.com/hello-pangea/dnd/blob/main/docs/guides/reparenting.md

ZyRealm currently works around that problem in two unrelated ancestors instead of isolating the drag layer:

1. `VirtualizedGrid.tsx` uses `top: virtualRow.start` rather than `transform: translateY(...)`, with an explicit comment saying this avoids re-anchoring a fixed DnD descendant.
2. `dialog.tsx` centers dialogs with `fixed inset-0 m-auto` rather than a translate transform, again with an explicit DnD fixed-position comment.

The reported jump/misalignment shows these ancestor-by-ancestor exceptions have not eliminated the coordinate-system problem. Continuing to ban transforms from every possible ancestor is brittle.

### Virtualization is paying a performance cost for the DnD workaround

TanStack Virtual's current examples use absolute rows positioned with `transform: translateY(...)`, including dynamic measurement examples:

- https://tanstack.com/virtual/latest/docs/framework/react/examples/dynamic
- https://tanstack.com/virtual/latest/docs/api/virtualizer

ZyRealm instead uses `top`, specifically to accommodate the in-tree fixed draggable. Once the dragged element is correctly cloned outside the virtualized DOM tree, the virtual row no longer needs to carry that DnD constraint.

### Group rows are dynamically measured despite a mostly fixed product layout

`Group/index.tsx` passes `estimateItemHeight={520}`. `VirtualizedGrid` then measures rendered rows through `measureElement`.

But a Group card's actual height can change because it contains:

- wrapping mode buttons;
- an optional Group Health block;
- a fixed `h-96` member viewport;
- card-level controls and animation trees.

This creates a loop where the virtualizer starts from an estimate, mounts cards, measures the actual row, and updates later row positions. Refreshes/remounts can repeat that process. This is a plausible structural contributor to the reported visible vertical repositioning/flicker.

The correct first-line fix is to make the Group card footprint deterministic rather than adding more measurement heuristics.

### Per-card subscriptions multiply render work

Each visible `GroupCard` currently calls `useModelChannelList()` independently. Each visible `GroupHealthBadge` calls `useGroupHealthList()` independently.

React Query shares the network request by query key, but every mounted hook is still a query observer and every card still performs its own derived work. Current refresh periods include:

- `useGroupList()`: 30 s;
- `useModelChannelList()`: 30 s;
- `useGroupHealthList()`: 30 s, or 5 s while any group-health run is active.

Therefore one background refresh can cause many visible cards to re-render and can interact with dynamic virtual-row measurement.

### Slice A decision

Use the DnD library's supported cloning API instead of additional CSS exceptions, then restore a compositor-friendly virtual-row transform and make Group rows fixed-height. Lift shared model-channel data to the Group page so cards receive already-indexed metadata.

Do **not** switch DnD libraries in this slice. Do **not** add inner member-list virtualization in the first pass. Those are follow-up options only if measurements after the structural correction show a remaining large-list bottleneck.

Detailed implementation plan:

- [`2026-09-17-group-ui-performance-dnd-plan.md`](./2026-09-17-group-ui-performance-dnd-plan.md)

---

## B. Auto Add audit

### Two different concepts currently share the words “auto add / auto group”

There is a global/per-channel **Auto Group** system in:

- `web/src/components/modules/group/AutoGroupDialog.tsx`
- `internal/op/group_auto_group.go`

That system controls whether channel model inventories are automatically projected into Groups using None / Fuzzy / Exact / Regex policies.

Separately, `GroupEditor` contains a local **Auto Add** button. That button only computes matching members in the browser and appends them to `selectedMembers`; the user must still save the editor.

Current local matching is:

```text
if group.match_regex exists:
    match model name against regex
else:
    lowercase model name contains lowercase group name
```

This local action is useful, but it is inaccessible from the Group card.

### Stale frontend API must not simply be resurrected

`web/src/api/endpoints/group.ts` still contains a commented-out hook for:

```text
POST /api/v1/group/auto-add-item
```

The current backend does not expose that route. Source history inspected in this audit also does not establish a safe current server contract for it.

Therefore the new card quick action should be designed from current Group semantics rather than uncommenting dead client code.

### Existing backend pieces are reusable, but persistence must stay on `GroupUpdate`

`internal/op/channel.go` already provides `ChannelLLMList(ctx)` as the authoritative channel-model inventory.

`internal/op/group.go` also contains `GroupItemBatchAdd(...)`, which demonstrates existing duplicate/priority/cache semantics. However, it is **not** the correct final persistence entry point for this quick action because `GroupUpdate()` additionally executes `syncActivePresetTx`, preserving the current live-binding invariant between a Group and its active preset.

The missing piece is therefore a thin server-side **resolve matches for this persisted Group** operation followed by `GroupUpdate(ItemsToAdd=...)`, not a second item-write path.

### Slice B decision

Add an idempotent persisted-Group quick action:

```text
POST /api/v1/group/:id/auto-add
  -> load Group
  -> resolve matches using persisted group.name / group.match_regex
  -> subtract existing items
  -> GroupUpdate(items_to_add=...)
  -> preserve active preset live binding
  -> return updated Group + matched/added/already-present counts
```

The Group-card action sits with Copy / Preset / Protocol / Edit. The editor keeps its local preview behavior for unsaved name/regex values, but the matching rules should be extracted into a pure helper and explicitly treated as preview semantics; persisted execution remains backend-authoritative.

Detailed implementation plan:

- [`2026-09-17-group-auto-add-quick-action-plan.md`](./2026-09-17-group-auto-add-quick-action-plan.md)

---

## C. Group Health / Full Probe audit

### Current Group Health is synthetic inference, not passive health

`internal/grouphealth/probe.go` builds a real upstream model request. The payload is currently fixed:

```text
prompt/input = "ping"
max_tokens / max_completion_tokens = 1
stream = false
```

Embeddings also use `"ping"` as input. Any HTTP 2xx is treated as success.

This means Group Health consumes an actual provider request and can be blocked by providers that reject synthetic/automated probes.

### “Standard” and “Full” are not different probe protocols

Both modes send the same synthetic request.

The difference is traversal scope:

- **standard** in a Failover group stops after the first successful candidate;
- **full** continues through all candidates.

Thus “Full” currently means “send the same active probe to more upstream candidates”, not “perform a deeper or more trustworthy health check”.

### Manual Group Health does not numerically feed HealthFirst

Actual routing health is based on real traffic:

```text
relay final result
  -> outlierwindow.Report / ReportChannel
  -> outlierwindow.Evaluate(channel, model)
  -> balancer.itemHealthScore(...)
  -> HealthFirst / same-priority Failover ordering
```

Relevant files:

- `internal/outlierwindow/window.go`
- `internal/relay/balancer/balancer.go`
- `internal/relay/balancer/health_order.go`

`GroupHealthSnapshot` is a separate persisted diagnostic record and is not the input to `itemHealthScore`.

Therefore a user pressing the Group Health button does **not** directly increase or decrease the HealthFirst score.

### But the same synthetic Prober is reused by POR

`internal/task/site_outlier.go` reuses `grouphealth.Prober` for Passive Outlier Retirement (POR):

1. passive real-traffic window identifies a candidate;
2. sibling evidence is checked;
3. the synthetic active probe is used as a confirmation gate;
4. a failed probe can lead to managed-channel retirement/disable;
5. the same probe is also used for recovery.

So the accurate answer is:

- with POR disabled (default), Group Health is diagnostic-only;
- Group Health snapshots do not feed the routing health score;
- if POR is enabled, the **same active probing mechanism** can influence channel availability through retire/recover control decisions.

This coupling is too strong for a fixed `"ping"` request, especially with multi-key channels, model-specific policy errors, provider anti-probe rules, rate limits, and content restrictions.

### Changing `ping` to `OK` / `pong` is not a sufficient fix

Changing one fixed synthetic string to another keeps the underlying problem:

- it remains synthetic inference traffic;
- it remains identifiable/rejectable by an upstream;
- one model/prompt policy response is not proof of whole-channel failure;
- one selected credential failure is not proof that a multi-key channel is dead.

### Slice C decision

Separate two concepts explicitly:

1. **Passive routing health** — real client traffic only; authoritative for HealthFirst, failover health ordering, outlier evidence, and circuit behavior.
2. **Active diagnostic probe** — opt-in/manual diagnostic evidence; never directly changes passive health score.

Immediate hardening should also stop POR from interpreting every synthetic probe failure as proof of channel unavailability. Probe results need a bounded classification so policy/unsupported/rate-limit/credential-scoped/inconclusive outcomes cannot retire a whole channel.

The Group UI should be renamed/reframed as diagnostic probing. The primary card UI should remove/demote Full Probe; backend compatibility may remain temporarily while external usage is audited.

POR remains default-off throughout this slice.

Detailed implementation plan:

- [`2026-09-17-health-probe-decoupling-plan.md`](./2026-09-17-health-probe-decoupling-plan.md)

---

## Recommended execution order

```text
A. Group UI performance + DnD correctness
   ↓
B. Card-level Auto Add quick action
   ↓
C. Health probe semantic hardening / POR decoupling
```

Reasoning:

- A directly addresses the current usability regression and is frontend-contained.
- B adds a Group-card action after the card/rendering structure is stable.
- C touches availability/control-plane safety and should be reviewed independently from UI performance work.

Each slice requires its own RED/GREEN evidence and final CI gate. No slice should be deployed automatically as part of plan creation.