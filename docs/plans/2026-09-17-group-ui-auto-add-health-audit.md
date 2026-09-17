# Group UI / Auto Add / Health Diagnostics Audit

**Audit baseline:** `main@6978f0cc02770b5ca94fafd85914130471f40442`

**Scope:** investigate five user-visible problems without changing production behavior: group-page lag, member drag offset, scroll flicker, per-group Auto Add UX, and active probe semantics.

## Executive decision

This is not one defect and must not be shipped as one broad patch. Split follow-up work into three independently reviewable tracks:

1. **Group UI stability** — render containment, shared-query hoisting, drag reparenting, and group-grid virtualization policy.
2. **Per-group Auto Add UX** — reuse the editor's existing matching semantics, add a card-header shortcut with preview/confirm, and keep the backend group model unchanged.
3. **Operational health vs active diagnostics** — make real-traffic runtime health authoritative, keep synthetic probing manual and non-authoritative, and stop presenting the latest synthetic probe as if it were routing health.

Do not change relay routing, database schema, or deployment configuration as part of the UI-stability or Auto Add tracks.

---

## 1. Group page lag and flicker: source audit

### Current render topology

`web/src/components/modules/group/index.tsx` always renders groups through `VirtualizedGrid` with a dynamic height estimate of `520` px. The virtualizer owns an `overflow-y-auto` scroll container and dynamically measures visible rows.

Every visible `GroupCard` then performs work that should be page-scoped:

- `GroupCard` calls `useModelChannelList()` itself.
- Every card iterates the complete model-channel list and rebuilds `modelChannelByKey`.
- Every card renders `GroupHealthBadge`.
- Every `GroupHealthBadge` calls `useGroupHealthList()` and scans the complete group-health list to find its own group.
- `useGroupList()`, `useModelChannelList()`, and `useGroupHealthList()` are independently refreshed by React Query; group and health list polling are currently 30 s in the idle state.

The result is a multiplicative update pattern: a shared data refresh can wake many card observers, rebuild repeated maps, and rerender expensive card/member trees while the outer virtualizer is also mounting/unmounting rows during scroll.

### Why this matches the reported symptoms

The group card is not a lightweight row. It contains:

- an inline drag-and-drop context;
- all visible member rows in a fixed `h-96` list;
- tooltips/popovers;
- health UI;
- motion-based delete confirmation;
- an edit morphing dialog.

Unmount/remount churn is therefore materially more expensive than the normal use case for a generic virtualized card grid. This is a credible structural explanation for both scroll jank and visible re-entry flicker.

### Decision

Do not start by tuning `overscan`, animation duration, or `estimateItemHeight`. First remove repeated page-wide subscriptions/work from each card. Then use a Group-specific adaptive rendering policy: normal CSS grid for ordinary group counts and virtualization only for large sets.

Initial policy for implementation and testing:

- `<= 32` visible groups: plain responsive CSS grid; no `VirtualizedGrid`.
- `> 32` visible groups: existing `VirtualizedGrid`, after shared data has been hoisted.
- threshold lives in one exported constant so later profiling can change it without changing architecture.

The value `32` is a conservative first boundary: at four columns it is eight card rows, while avoiding virtual mount churn for the common small/medium control-plane page. It is not a routing or data-model semantic.

---

## 2. Member drag offset / "block jumps to screen center": source audit

### Current topology

`web/src/components/modules/group/ItemList.tsx` uses `@hello-pangea/dnd@18.0.1`.

The draggable member list has its own `overflow-y-auto` container. On the normal group page it is nested inside `VirtualizedGrid`, whose own container is also `overflow-y-auto`.

The repository already contains defensive comments/workarounds for the classic fixed-position drag issue:

- virtual rows use absolute `top` instead of `transform: translateY(...)` so they do not establish a containing block for fixed descendants;
- dialog content uses `fixed inset-0 m-auto` instead of translate-based centering for the same reason.

Those mitigations are evidence that the coordinate problem has already been treated at the CSS symptom level. The user's current report means that continuing to patch `transform/top/z-index` is not an adequate strategy.

### Library constraint

`@hello-pangea/dnd` moves a dragging item using fixed positioning and documents reparenting/cloning for cases where ancestor layout/transform interferes. It also documents support for a droppable that is the scroll container or has one scrollable parent; arbitrary nested scroll containers are not a supported layout model.

### Decision

Use the library's first-class clone/reparent mechanism:

- render the active drag clone outside the virtual/card DOM tree, into `document.body`;
- keep source and clone dimensions/styles identical;
- add a drag lifecycle signal from `MemberList` upward so the outer group scroll surface does not perform disruptive layout/measurement changes during an active drag;
- do not migrate drag libraries in the first fix.

A library migration is a fallback only if clone + render containment + scroll coordination still fails the acceptance matrix.

---

## 3. Auto Add: source audit

There are two different concepts currently sharing similar wording and they must remain distinct.

### A. Editor-local per-group Auto Add

`GroupEditor` computes candidate members from the current group's name / `match_regex` and the current `modelChannels` list. Clicking Auto Add only appends non-duplicate matches to local editor state with `weight=1`; persistence happens when the edit form is saved.

This is the feature the requested card-header shortcut should expose.

### B. Channel Auto Group orchestration

`internal/op/group_auto_group.go` implements channel-level/global auto-group configuration, including projected/managed channels. It is a separate orchestration feature and should not be reused as the card button's mutation path.

### Decision

Extract the editor's matching algorithm into one pure helper and use it from both entry points.

Add a per-group card action beside Copy / Preset / Protocol Policy. Clicking it opens a lightweight preview showing:

- matching basis: group-name matching or regex;
- number of new members;
- matched channel-model pairs;
- no-match / invalid-regex state.

Confirmation sends an ordinary `GroupUpdateRequest.items_to_add` using the existing group update endpoint. Existing `(group_id, channel_id, model_name)` conflict protection remains the backend safety net.

Do not silently add members on the first click.

---

## 4. "Health" and "Full Probe": source audit

### What the current probe actually does

`internal/grouphealth/probe.go` performs a real provider request. For generative protocols it sends a user message containing the literal string `"ping"`, with streaming disabled and a one-token completion limit. Embeddings use `"ping"` as input.

A 2xx response is considered success. The probe does not validate a `pong` response body.

`standard` and `full` are not different probe protocols. They differ primarily in traversal semantics:

- for **Failover** groups, standard stops after the first successful candidate and marks the rest skipped;
- full probes every candidate;
- for non-Failover modes, standard already traverses every candidate.

This naming therefore overstates the distinction.

### Does active probing participate in routing health?

**No.**

Active group probes persist only `GroupHealthSnapshot` / `GroupHealthAttempt` records. They do not report into the relay's `outlierwindow`, credential availability, or circuit breaker.

The actual `HealthFirst` and Failover health ordering uses `itemHealthScore(channelID, modelName, now)`, which is calculated from `outlierwindow.Evaluate(...)` over **real relay request results**. With no samples it returns neutral `0.5`; otherwise it uses failure rate, consecutive-failure penalty, and sample-count conservatism. Circuit state is handled separately by the relay balancer.

Therefore the current UI label is semantically misleading: the card's "health" display is the latest **active synthetic diagnostic snapshot**, not the health evidence used by routing.

### Decision

Preserve this separation and make it explicit:

- **Operational Health / 运行健康** = passive real-traffic evidence used by routing. Authoritative.
- **Active Diagnostic / 主动诊断** = manually initiated smoke test. Informational only.

Active diagnostics must never modify HealthFirst score, cooldown, availability, or circuit state by default.

Do not try to solve provider anti-probe behavior merely by changing `ping` to `ok`/`pong`. The primary fix is semantic: a rejected synthetic diagnostic is not proof that a channel is unhealthy.

For the retained manual inference smoke test, replace the recognizable `ping` probe text with a tiny ordinary request (`"What is 1+1?"`, non-streaming, max two output tokens). Its output content is not graded; only request reachability/2xx matters. This reduces obvious health-probe signatures while retaining an end-to-end inference test. Keep it manual-only.

The detailed health plan also adds a read-only Operational Health view sourced from the existing `outlierwindow` + circuit state, without feeding diagnostic results back into routing.

---

## 5. Delivery order

### P0 — Group UI stability

Must land first because it addresses the current unusable interaction surface and also reduces noise before evaluating the other UI changes.

Plan: `docs/plans/2026-09-17-group-ui-stability-plan.md`

### P1 — Per-group Auto Add shortcut

Land after P0 so the new header action is implemented on a stable card component.

Plan: `docs/plans/2026-09-17-group-auto-add-ux-plan.md`

### P2 — Health semantics / diagnostics

Land separately because it changes product semantics and exposes runtime-health data, even though it should not alter routing decisions.

Plan: `docs/plans/2026-09-17-group-health-diagnostics-plan.md`

---

## Global non-goals

- No local Mac build/deploy/install.
- No DB migration for P0/P1.
- No relay selection-policy change in any of these tracks.
- No active-probe result feeding into `outlierwindow`, cooldown, credential availability, or circuit breaker.
- No broad DnD-library migration unless the targeted structural fix fails its acceptance gate.
- No replacement of the existing channel/global Auto Group subsystem.
