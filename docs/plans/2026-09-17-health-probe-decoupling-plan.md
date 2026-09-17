# Active Probe / Routing Health Decoupling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make ZyRealm's routing health unambiguously real-traffic-driven, demote synthetic Group Health requests to explicit diagnostics, and prevent provider probe rejection from becoming automatic whole-channel retirement evidence.

**Architecture:** Keep `outlierwindow` + circuit/runtime evidence as the authoritative passive health source. Preserve the existing synthetic `grouphealth.Prober` only as an explicit diagnostic/confirmation mechanism, but classify its outcomes so POR may act only on genuine upstream-unavailable evidence; credential/policy/model/rate-limit/probe-rejection outcomes are inconclusive for whole-channel retirement. The Group card stops continuously polling/rendering diagnostic snapshots and instead exposes an on-demand diagnostic action.

**Tech Stack:** Existing Go `grouphealth`, POR task, balancer/outlierwindow, React/TanStack Query Group UI.

**Spec:** [`2026-09-17-group-ux-autoadd-health-audit.md`](./2026-09-17-group-ux-autoadd-health-audit.md)

## Global Constraints

- Baseline: `main@6978f0cc02770b5ca94fafd85914130471f40442` plus prior completed Group slices if executed first.
- `HealthFirst` and same-priority Failover health ordering remain based on `outlierwindow` real-request evidence.
- Manual diagnostic runs must not write `outlierwindow`, circuit-breaker state, credential cooldown, or routing score.
- `SettingKeyGroupHealthEnabled` remains default `false`.
- `SettingKeyOutlierRetireEnabled` remains default `false`.
- Do not claim that changing `ping`/`pong`/`OK` makes active probing provider-safe.
- Do not add a new hidden/obfuscated probe phrase intended to evade provider detection.
- Do not retire a whole channel from a single credential-specific active-probe failure.
- Do not add a DB migration in this slice.
- Keep `probe_mode=full` API compatibility during this slice; remove it from the primary UI, not from the wire contract.
- No local Mac build/deploy; repository CI is authoritative.

---

## Current semantics to preserve or correct

### Authoritative routing health

```text
real relay request result
  -> outlierwindow.Report / ReportChannel
  -> outlierwindow.Evaluate(channel, model)
  -> itemHealthScore
  -> HealthFirst / Failover ordering
```

This remains unchanged.

### Current active probe

`internal/grouphealth/probe.go` sends a real synthetic model request using `"ping"` and a one-token output budget. Standard and Full use the same request; Full merely traverses more candidates.

### Unsafe coupling to correct

`internal/task/site_outlier.go` reuses the same Prober. Today a generic `ProbeResult.Success == false` can confirm retirement after passive outlier evidence. That is too coarse because failures include credential rejection, provider anti-probe policy, model/path incompatibility, rate limiting, and other results that are not proof that the whole channel is unavailable.

---

## File Structure

**Create**

- `internal/grouphealth/probe_outcome.go`
  - bounded active-probe outcome taxonomy and HTTP/transport classifier.
- `internal/grouphealth/probe_outcome_test.go`
  - classification coverage.
- `web/tests/group-diagnostic-ui.test.mjs`
  - source contract: on-demand diagnostic action, no card-level list polling, no Full button.

**Modify**

- `internal/grouphealth/probe.go`
  - populate classified outcome while keeping existing response evidence.
- `internal/task/site_outlier.go`
  - POR confirmation/recovery decisions consume classified outcomes conservatively.
- `internal/task/site_outlier_test.go`
  - retirement safety regressions.
- `internal/grouphealth/probe_test.go`
  - prove active diagnostics remain passive-health neutral.
- `web/src/components/modules/group/health.tsx`
  - refactor always-mounted health badge into on-demand diagnostic action/dialog.
- `web/src/components/modules/group/Card.tsx`
  - move diagnostic trigger into compact header actions and remove the large always-rendered health block.
- `web/src/api/endpoints/group-health.ts`
  - use detail query on demand; keep compatibility APIs for list/full mode.
- existing Group locale messages
  - rename user-facing semantics from route “health” to “diagnostic probe” and add provider-warning copy.

No `internal/relay/balancer/*` production file should change unless a RED regression proves the passive-health invariant is already violated.

---

### Task 1: Lock passive-health neutrality and probe taxonomy with RED tests

**Files:**
- Create: `internal/grouphealth/probe_outcome_test.go`
- Modify: `internal/grouphealth/probe_test.go`
- Create later: `internal/grouphealth/probe_outcome.go`

**Interfaces:**
- Planned taxonomy:

```go
type ProbeOutcome string

const (
    ProbeOutcomeSuccess            ProbeOutcome = "success"
    ProbeOutcomeUnavailable        ProbeOutcome = "unavailable"
    ProbeOutcomeCredentialRejected ProbeOutcome = "credential_rejected"
    ProbeOutcomeRateLimited        ProbeOutcome = "rate_limited"
    ProbeOutcomeRejected           ProbeOutcome = "rejected"
    ProbeOutcomeInconclusive       ProbeOutcome = "inconclusive"
)
```

- `ProbeResult` gains:

```go
Outcome ProbeOutcome
```

`Success` remains for compatibility and must equal `Outcome == ProbeOutcomeSuccess`.

- [ ] **Step 1: Add table-driven RED classification tests**

Required cases:

```text
2xx                    -> success
401                    -> credential_rejected
403                    -> rejected
404                    -> rejected
400 / 405 / 409 / 422  -> rejected
429                    -> rate_limited
408                    -> unavailable
500 / 502 / 503 / 504  -> unavailable
other 4xx              -> rejected
parent context canceled -> inconclusive
probe deadline/transport connect/reset failure -> unavailable
```

`403` is deliberately **not** whole-channel unavailable: it may be WAF/policy/probe rejection. Cloudflare fingerprints remain diagnostic metadata but do not override this safety rule.

- [ ] **Step 2: Add a RED invariant that a manual Prober call does not populate passive health**

Build an `httptest.Server`, execute `Prober.RunCandidate(...)`, then assert:

```go
stats := outlierwindow.Evaluate(channel.ID, modelName, time.Now())
if stats.Samples != 0 {
    t.Fatalf("active diagnostic must not enter passive health window")
}
```

Reset `outlierwindow` test state using its existing reset/test helper or isolated channel/model IDs according to current package facilities.

- [ ] **Step 3: Run focused tests and verify RED**

```bash
go test ./internal/grouphealth -run 'TestProbeOutcome|TestProberDoesNotReportPassiveHealth' -count=1
```

Expected: taxonomy tests fail because `ProbeOutcome` is not implemented. Passive-neutral test should either already pass or expose an unexpected coupling; do not weaken it if it passes in the RED commit.

- [ ] **Step 4: Commit tests**

```bash
git add internal/grouphealth/probe_outcome_test.go internal/grouphealth/probe_test.go
git commit -m "test(health): define active probe outcome safety"
```

---

### Task 2: Implement a bounded active-probe classifier without changing the probe phrase

**Files:**
- Create: `internal/grouphealth/probe_outcome.go`
- Modify: `internal/grouphealth/probe.go`
- Test: `internal/grouphealth/probe_outcome_test.go`

**Interfaces:**
- Add helpers equivalent to:

```go
func classifyProbeHTTP(status int) ProbeOutcome
func classifyProbeTransport(parentErr, probeErr error) ProbeOutcome
```

- [ ] **Step 1: Implement HTTP classification**

Use explicit status families rather than error-message substring matching:

```go
switch {
case status >= 200 && status < 300:
    return ProbeOutcomeSuccess
case status == 401:
    return ProbeOutcomeCredentialRejected
case status == 429:
    return ProbeOutcomeRateLimited
case status == 408 || status >= 500:
    return ProbeOutcomeUnavailable
case status >= 400 && status < 500:
    return ProbeOutcomeRejected
case status == 0:
    return ProbeOutcomeInconclusive
default:
    return ProbeOutcomeInconclusive
}
```

A provider-specific WAF/policy 403 remains `rejected`, not `unavailable`.

- [ ] **Step 2: Implement transport/context classification**

If the parent operation is already canceled, return `inconclusive`; an administrative shutdown/caller cancel must not become provider-health evidence.

A probe's own deadline, DNS/connect/TLS/reset/EOF-before-response class of transport failure may return `unavailable`. Keep the implementation bounded to typed errors/context state; do not add a free-form provider-message taxonomy.

- [ ] **Step 3: Populate `ProbeResult.Outcome` in every exit path**

Rules:

```text
request construction / local config error -> inconclusive
custom param override error              -> inconclusive
HTTP client construction error           -> inconclusive
parent canceled                           -> inconclusive
probe timeout / transport unavailable     -> unavailable
HTTP response                             -> classifyProbeHTTP(status)
```

Set:

```go
result.Success = result.Outcome == ProbeOutcomeSuccess
```

Do not change the fixed `"ping"` payload in this task. The point of this slice is to remove authority from synthetic wording, not invent a different phrase.

- [ ] **Step 4: Run grouphealth tests**

```bash
go test ./internal/grouphealth -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/grouphealth/probe.go internal/grouphealth/probe_outcome.go internal/grouphealth/probe_outcome_test.go internal/grouphealth/probe_test.go
git commit -m "feat(health): classify active probe outcomes"
```

---

### Task 3: Prevent inconclusive synthetic probes from retiring channels

**Files:**
- Modify: `internal/task/site_outlier.go`
- Modify: `internal/task/site_outlier_test.go`

**Interfaces:**
- POR confirmation rule becomes:

```text
success       -> clear passive window / do not retire
unavailable   -> may confirm retirement, but only after existing passive + sibling gates
all other outcomes -> inconclusive; do not retire / disable
```

- [ ] **Step 1: Add POR RED regressions**

Using the existing injectable `channelProber`, add cases proving that after passive Gate 1/Gate 2 evidence:

```text
credential_rejected -> channel remains enabled
rate_limited         -> channel remains enabled
rejected (403/404)   -> channel remains enabled
inconclusive         -> channel remains enabled
unavailable (503/transport) -> existing retirement flow may proceed
success              -> passive window is cleared and channel remains enabled
```

For site-level outage, the same conservative rule must apply: one rejected/inconclusive probe cannot disable all siblings.

- [ ] **Step 2: Verify RED**

```bash
go test ./internal/task -run 'Test.*Outlier.*Probe|Test.*SiteOutage' -count=1
```

Expected: at least the new non-success/non-unavailable cases fail under the current boolean-only logic.

- [ ] **Step 3: Change retirement confirmation to an explicit outcome switch**

`retireViaProbe` and `handleSiteOutage` must use an explicit switch:

```go
switch res.Outcome {
case grouphealth.ProbeOutcomeSuccess:
    // current success behavior
case grouphealth.ProbeOutcomeUnavailable:
    // current confirmed-retirement behavior
case grouphealth.ProbeOutcomeCredentialRejected,
     grouphealth.ProbeOutcomeRateLimited,
     grouphealth.ProbeOutcomeRejected,
     grouphealth.ProbeOutcomeInconclusive:
    // log bounded diagnostic context and return without disabling
}
```

Do not reinterpret a 401 from one selected key as a whole-channel failure in a channel that may hold many credentials.

- [ ] **Step 4: Keep recovery conservative**

`recoverRetired` continues to re-enable only on `ProbeOutcomeSuccess`.

All other outcomes leave the retired state unchanged; they must not increment a recovery-success streak. This slice does not invent a half-open real-traffic recovery mechanism.

Add a code comment identifying that **half-open real-request recovery is the preferred future replacement for synthetic-only recovery where providers reject probes**.

- [ ] **Step 5: Run task tests**

```bash
go test ./internal/task -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/task/site_outlier.go internal/task/site_outlier_test.go
git commit -m "fix(por): ignore inconclusive active probes"
```

---

### Task 4: Turn Group Health from an always-mounted badge into an on-demand diagnostic action

**Files:**
- Create: `web/tests/group-diagnostic-ui.test.mjs`
- Modify: `web/src/components/modules/group/health.tsx`
- Modify: `web/src/components/modules/group/Card.tsx`
- Modify: `web/src/api/endpoints/group-health.ts`
- Modify: Group locale messages.

**Interfaces:**
- Replace the card body component with a compact action interface:

```tsx
<GroupDiagnosticAction groupId={group.id} />
```

- [ ] **Step 1: Add a RED source-contract test**

Assert:

```js
const card = await source('../src/components/modules/group/Card.tsx');
const health = await source('../src/components/modules/group/health.tsx');

assert.match(card, /GroupDiagnosticAction/);
assert.doesNotMatch(card, /<GroupHealthBadge/);
assert.doesNotMatch(health, /useGroupHealthList\(\)/);
assert.match(health, /useGroupHealth\(/);
assert.doesNotMatch(health, /runFull|probeMode:\s*['"]full['"]/);
```

The test should also verify provider-warning translation keys are used rather than a hard-coded English warning.

- [ ] **Step 2: Verify RED**

```bash
cd web
node --test tests/group-diagnostic-ui.test.mjs
```

Expected: FAIL because the current badge/list-polling/full-button UI remains.

- [ ] **Step 3: Refactor the component to query only when its dialog is open**

Use:

```ts
const [open, setOpen] = useState(false);
const { data: view } = useGroupHealth(open ? groupId : null);
```

The compact trigger is always available only when the existing `group_health_enabled` setting is enabled. It should not start a 30-second list poll for every card.

Place the trigger in the Group-card compact action row with the same footprint as Copy/Preset/Protocol/Edit.

- [ ] **Step 4: Remove Full Probe from primary UI**

Keep one explicit action:

```text
Run diagnostic
```

The dialog must explain before/near the button:

```text
- this sends a real synthetic request to the provider;
- it may consume quota/cost and may be blocked by provider policy;
- it is diagnostic evidence, not the routing HealthFirst score.
```

Do not expose `Run Full` in the card/dialog primary UI. Keep the backend/API `full` mode type and endpoint compatibility in this slice so external/manual callers are not silently broken.

- [ ] **Step 5: Keep historical attempt details read-only**

The existing attempt list/status/HTTP/duration/error display can remain inside the on-demand dialog. Rename headings from ambiguous “health” wording to diagnostic/probe wording in all three locales.

- [ ] **Step 6: Run frontend tests**

```bash
cd web
node --test tests/*.test.mjs
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/modules/group/health.tsx web/src/components/modules/group/Card.tsx web/src/api/endpoints/group-health.ts web/tests/group-diagnostic-ui.test.mjs public/locales
git commit -m "refactor(group): make health probes on-demand diagnostics"
```

If the authoritative locale path differs on the implementation baseline, modify the existing locale sources rather than creating a parallel catalog.

---

### Task 5: Explicitly preserve backend Full-mode compatibility and passive-health isolation

**Files:**
- Modify tests only unless regression is found:
  - `internal/grouphealth/service_test.go`
  - `internal/server/handlers/group_health.go` / tests if necessary.

- [ ] **Step 1: Lock wire compatibility**

Keep tests proving:

```text
POST /api/v1/group/health/:id/run {} -> standard
POST /api/v1/group/health/:id/run {"probe_mode":"full"} -> accepted full
invalid probe_mode -> bad request
```

The UI hiding Full is not an API removal.

- [ ] **Step 2: Lock Standard/Full semantics in documentation/test names**

Tests should describe the actual difference:

```text
standard + Failover: stop after first candidate success
full: traverse all candidates
```

Do not describe Full as a stronger health signal.

- [ ] **Step 3: Run grouphealth/handler tests**

```bash
go test ./internal/grouphealth ./internal/server/handlers -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit any test-only clarification**

```bash
git add internal/grouphealth/service_test.go internal/server/handlers
git commit -m "test(health): preserve probe mode compatibility"
```

Skip this commit if existing tests already fully lock the contract and no file needs modification.

---

### Task 6: Full safety and regression gate

- [ ] **Step 1: Run focused backend suites**

```bash
go test ./internal/grouphealth ./internal/outlierwindow ./internal/relay/balancer ./internal/task ./internal/server/handlers -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full backend suite**

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 3: Run frontend tests/lint/build through repository CI**

Required:

```text
node --test tests/*.test.mjs: PASS
lint: PASS
production build: PASS
```

- [ ] **Step 4: Safety acceptance**

Verify:

```text
[ ] Running a manual Group diagnostic leaves outlierwindow samples unchanged.
[ ] A diagnostic 401 does not cool down/disable a whole channel.
[ ] A diagnostic 403/404/429 does not retire a channel or a site account.
[ ] A probe-unavailable result can only retire after the pre-existing passive POR gates are already satisfied.
[ ] HealthFirst ordering is unchanged when only diagnostic snapshots change.
[ ] Group page no longer polls group-health list from every mounted card.
[ ] Full Probe is absent from the primary Group UI.
[ ] Diagnostic copy clearly states real request / quota / provider-policy risk.
[ ] POR remains default-off.
```

- [ ] **Step 5: Final repository CI gate**

Required:

```text
governance: PASS
backend Vet/full tests: PASS
frontend lint/test/build: PASS
```

- [ ] **Step 6: Record actual implementation head and CI evidence only after verification**

No deployment is implied.

---

## Deferred follow-up: half-open recovery without synthetic-only probes

This slice deliberately does not redesign retired-channel recovery. A provider that rejects all synthetic inference probes may remain retired until manually restored because recovery still requires an active success.

The preferred future source-audit is a **half-open real-request recovery path** analogous to circuit-breaker recovery:

```text
retired candidate
  -> bounded half-open admission of real client request
  -> real success restores
  -> real failure keeps retired/backoff
```

That design changes control-plane and request-admission semantics and must be planned separately. It must not be smuggled into the diagnostic UI hardening PR.