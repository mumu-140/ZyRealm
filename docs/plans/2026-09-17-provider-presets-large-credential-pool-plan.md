# Provider Presets + Large Credential Pool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a thin Provider Presets layer to Channel creation, including NVIDIA NIM and AMD AIM/ROCm, while making large credential pools such as ~100 NVIDIA API keys practical without replacing ZyRealm's existing fair credential scheduler.

**Architecture:** Presets are control-plane templates only. A selected preset pre-fills the existing `ChannelFormData` (`type`, base URL, model-discovery behavior, concurrency hint, etc.) and then hands control back to the normal Channel creation flow. Runtime routing remains the existing Channel -> credential fairness/cooldown/circuit-breaker -> protocol attempt pipeline. Large key pools reuse the current equal-weight fair ledger and gain only UI import ergonomics plus a narrowly bounded credential-failover adjustment.

**Tech Stack:** Next.js/React/TypeScript frontend, existing Channel API types, Go relay runtime, existing credential fairness/cooldown machinery, current repository CI/governance.

**Spec:** `docs/plans/2026-09-14-octopus-gap-roadmap.md` plus the 2026-09-17 provider-preset / large-key-pool source audit.

## Global Constraints

- Do not add a second provider-routing domain. Provider presets must compile down to the existing Channel model.
- Do not add a database migration for presets in this slice.
- Do not replace `availability.SelectCredentialFair`, credential cooldown, circuit breaking, failure-domain classification, replay-safety, or provider failover.
- Do not hard-code provider model lists. Continue using the existing model-fetch / sync path so upstream model availability stays authoritative.
- NVIDIA hosted preset uses the OpenAI-compatible API Catalog/NIM endpoint `https://integrate.api.nvidia.com/v1`.
- AMD AIM/ROCm is a deployment-oriented OpenAI-compatible preset. Do not invent a universal hosted AMD base URL; require an editable user-supplied endpoint.
- A large key pool is for distributing independent requests across available credentials. A single request must never sweep all credentials.
- Do not assume 100 keys imply 100x quota; upstream account/project/model limits may be shared.
- No local Mac build/deploy. Use the repository's existing CI contract and remote execution policy.

---

## Source-Audited Baseline

The current frontend already represents a Channel with all fields needed by presets. `Create.tsx` initializes `ChannelFormData` with `OpenAIChat`, one base URL row, one key row, `max_concurrency: 3`, and `max_rpm: 0`; the submit path normalizes the existing arrays rather than requiring provider-specific request types.

The API layer already exposes protocol types such as `ChannelType.OpenAIChat`, `OpenAIResponse`, `Anthropic`, `Gemini`, `Volcengine`, and `OpenAIEmbedding`. Therefore NVIDIA, AMD, Groq, Cerebras, Together, Fireworks, DeepInfra, OpenRouter, SiliconFlow, and generic OpenAI-compatible providers should be represented as presets over existing protocols rather than new backend channel enums.

The relay already has provider-local equal-weight credential fairness. `selectFairChannelCredential` filters disabled/empty/excluded/cooldown/circuit-broken credentials and delegates to `availability.SelectCredentialFair`. The fairness ledger is per Channel/provider, tracks progress and recency, and admits newly added/revised credentials at the current watermark to avoid catch-up bursts.

Credential-scoped failures already trigger credential-local cooldown. The current request-level credential failover budget is `defaultMaxCredentialsPerProvider = 2`; this plan proposes increasing that narrow bound to 3 after RED/GREEN coverage, not making it proportional to pool size.

Channel concurrency and RPM are provider/Channel-level gates and are checked before credential selection. Therefore a 100-key NVIDIA pool can be scheduled fairly today, but the default `max_concurrency: 3` may prevent the pool from expressing useful aggregate concurrency unless the operator explicitly configures a larger safe value.

---

## Preset Catalog for the First Slice

Ship a small curated set that maps cleanly onto existing protocols:

1. `NVIDIA NIM` — `ChannelType.OpenAIChat`, base URL `https://integrate.api.nvidia.com/v1`, API key required, dynamic model discovery enabled/supported.
2. `AMD AIM / ROCm` — `ChannelType.OpenAIChat`, empty editable base URL, API key optional, dynamic model discovery attempted against the user endpoint when supported.
3. `OpenRouter` — OpenAI-compatible template.
4. `Groq` — OpenAI-compatible template.
5. `Cerebras` — OpenAI-compatible template.
6. `Together AI` — OpenAI-compatible template.
7. `Fireworks AI` — OpenAI-compatible template.
8. `DeepInfra` — OpenAI-compatible template.
9. `SiliconFlow` — OpenAI-compatible template.
10. `Custom OpenAI-compatible` — empty editable base URL and no provider-specific assumptions.

Anthropic, Gemini, and Volcengine already have native protocol types and can be added to the same registry once their preset metadata is source-audited. They are not required to block the first NVIDIA/AMD slice.

---

### Task 1: Define a typed provider-preset registry

**Files:**
- Create: `web/src/components/modules/channel/provider-presets.ts`
- Test: `web/src/components/modules/channel/provider-presets.test.ts`

**Interfaces:**
- Consumes: `ChannelType` from `web/src/api/endpoints/channel.ts`.
- Produces:
  - `type ProviderPresetID = 'nvidia' | 'amd-aim' | 'openrouter' | 'groq' | 'cerebras' | 'together' | 'fireworks' | 'deepinfra' | 'siliconflow' | 'custom-openai'`
  - `type ProviderPreset`
  - `PROVIDER_PRESETS: readonly ProviderPreset[]`
  - `getProviderPreset(id: ProviderPresetID): ProviderPreset`

`ProviderPreset` should remain UI metadata, not a persistence type:

```ts
export type ProviderPreset = {
    id: ProviderPresetID;
    name: string;
    description: string;
    type: ChannelType;
    baseUrl: string;
    baseUrlRequired: boolean;
    apiKeyRequired: boolean;
    modelDiscovery: 'openai' | 'manual';
    recommendedMaxConcurrency?: number;
};
```

Do not include a static `models` array.

- [ ] **Step 1: Write registry tests**

Cover at minimum:

```ts
expect(getProviderPreset('nvidia')).toMatchObject({
    type: ChannelType.OpenAIChat,
    baseUrl: 'https://integrate.api.nvidia.com/v1',
    apiKeyRequired: true,
    modelDiscovery: 'openai',
});

expect(getProviderPreset('amd-aim')).toMatchObject({
    type: ChannelType.OpenAIChat,
    baseUrl: '',
    baseUrlRequired: true,
    modelDiscovery: 'openai',
});
```

Also assert that no preset exposes a static model list.

- [ ] **Step 2: Run the focused frontend test and confirm RED**

Run the repository's existing frontend test command targeted at `provider-presets.test.ts`. Expected failure: registry module/symbols do not exist yet.

- [ ] **Step 3: Implement the minimal typed registry**

Use only source-audited provider metadata. Keep base URLs editable even when a default is supplied.

- [ ] **Step 4: Run the focused frontend test and confirm GREEN**

- [ ] **Step 5: Commit**

Commit message: `feat(channel): add provider preset registry`.

---

### Task 2: Add a preset picker to Channel creation only

**Files:**
- Create: `web/src/components/modules/channel/ProviderPresetPicker.tsx`
- Modify: `web/src/components/modules/channel/Create.tsx`
- Test: `web/src/components/modules/channel/ProviderPresetPicker.test.tsx`

**Interfaces:**
- Consumes: `PROVIDER_PRESETS` and `ProviderPresetID`.
- Produces: `ProviderPresetPicker({ onSelect, onCustom })` and a pure preset-to-form helper that returns a `ChannelFormData` seed.

Selection semantics:

- NVIDIA seeds `name: 'NVIDIA NIM'`, `type: OpenAIChat`, base URL `https://integrate.api.nvidia.com/v1`, one empty enabled key row, no static models, and a visible concurrency recommendation rather than silently assuming account limits.
- AMD seeds `name: 'AMD AIM / ROCm'`, `type: OpenAIChat`, an empty editable base URL, and no mandatory fake key.
- Custom OpenAI-compatible reproduces the current blank create form.
- After selection, the user remains in the existing `ChannelForm`; there is no provider-specific submission endpoint.
- Editing an existing Channel does not show the preset picker in this slice.

- [ ] **Step 1: Write interaction tests**

Assert that selecting NVIDIA pre-fills protocol/base URL and selecting AMD leaves base URL empty/editable. Assert that submitting still goes through the existing Channel creation mutation shape.

- [ ] **Step 2: Run focused tests and confirm RED**

- [ ] **Step 3: Implement `ProviderPresetPicker` and integrate it into `Create.tsx`**

Keep the existing default-reset behavior after successful creation.

- [ ] **Step 4: Run focused tests and confirm GREEN**

- [ ] **Step 5: Commit**

Commit message: `feat(channel): add provider preset picker`.

---

### Task 3: Add bulk credential import for large provider key pools

**Files:**
- Create: `web/src/components/modules/channel/key-import.ts`
- Create: `web/src/components/modules/channel/KeyBulkImport.tsx`
- Modify: `web/src/components/modules/channel/Form.tsx`
- Test: `web/src/components/modules/channel/key-import.test.ts`
- Test: `web/src/components/modules/channel/KeyBulkImport.test.tsx`

**Interfaces:**
- Produces `parseCredentialLines(input: string): Array<{ enabled: true; channel_key: string; remark: string }>`.
- Input format: one credential per line. Trim surrounding whitespace, drop blank lines, preserve first-seen order, and de-duplicate exact credential values.
- Imported keys feed the existing `ChannelFormData.keys` array; no new backend API is required.

Required UX:

```text
Bulk import keys
[ textarea: one key per line ]

100 valid / 2 duplicates / 3 blank lines ignored
[Import]
```

Never echo full credentials back in summary/toast text. Existing key controls remain available after import for enable/disable/remark edits.

- [ ] **Step 1: Write parser tests**

Cover 100 lines, blank lines, whitespace trimming, duplicate suppression, and stable order.

- [ ] **Step 2: Run parser test and confirm RED**

- [ ] **Step 3: Implement parser and bulk-import UI**

Do not add a bulk-key persistence endpoint. Convert imported lines to the existing `keys` array and let normal Channel creation persist them.

- [ ] **Step 4: Run focused frontend tests and confirm GREEN**

- [ ] **Step 5: Commit**

Commit message: `feat(channel): support bulk credential import`.

---

### Task 4: Make large-key-pool concurrency explicit instead of magical

**Files:**
- Modify: `web/src/components/modules/channel/provider-presets.ts`
- Modify: `web/src/components/modules/channel/ProviderPresetPicker.tsx`
- Modify: `web/src/components/modules/channel/Form.tsx` only if needed for helper copy
- Test: `web/src/components/modules/channel/ProviderPresetPicker.test.tsx`

**Interfaces:**
- A preset may expose `recommendedMaxConcurrency`, but the value is an operator-facing starting point, not a claim about upstream quota.

NVIDIA behavior:

- Do not silently derive Channel concurrency from key count.
- Show that Channel-level `max_concurrency` is the actual local admission gate.
- When many keys are imported while `max_concurrency` remains very low (for example 3), surface a non-blocking hint that the pool will still be capped by Channel concurrency.
- Never state that 100 keys provide 100x throughput or quota.

- [ ] **Step 1: Write the warning/recommendation test**

Example expectation: after importing 100 keys with `max_concurrency = 3`, the UI shows a non-blocking message explaining that Channel concurrency caps simultaneous requests.

- [ ] **Step 2: Run focused tests and confirm RED**

- [ ] **Step 3: Implement the minimal hint/recommendation behavior**

- [ ] **Step 4: Run focused tests and confirm GREEN**

- [ ] **Step 5: Commit**

Commit message: `feat(channel): clarify large key pool concurrency`.

---

### Task 5: Increase bounded credential failover from 2 to 3

**Files:**
- Modify: `internal/relay/credential_rotation.go`
- Modify/Create test: `internal/relay/credential_provider_failover_test.go`
- Re-run existing: `internal/relay/credential_fair_integration_test.go`
- Re-run existing: `internal/relay/credential_cooldown_policy_test.go`

**Interfaces:**
- Existing routing failure classification remains authoritative.
- Change only the per-request count of distinct credentials attempted within one provider after credential-scoped failures.

Target behavior:

```go
const defaultMaxCredentialsPerProvider = 3
```

Semantics:

- Attempt credential A.
- If existing routing logic classifies the failure as credential-scoped, cooldown/exclude A and try B.
- If B also fails credential-scoped, try C.
- After the third distinct credential failure, skip that provider according to the existing provider-failover/replay rules.
- Never continue through all credentials in the pool.
- Non-credential failures must not consume this credential-rotation path.

- [ ] **Step 1: Add a RED integration test with at least four eligible keys**

Make A and B return credential-scoped failures, C succeed, and D remain unused. Expected result before implementation: provider is skipped after B because the limit is 2.

- [ ] **Step 2: Run the focused Go test and confirm RED**

- [ ] **Step 3: Change the constant from 2 to 3**

No new retry policy, timeout rule, or failure classifier in this task.

- [ ] **Step 4: Run focused credential fairness/cooldown/failover tests and confirm GREEN**

- [ ] **Step 5: Commit**

Commit message: `feat(relay): allow third credential failover`.

---

### Task 6: Verify NVIDIA 100-key fairness as an invariant

**Files:**
- Modify/Create test: `internal/relay/availability/credential_fair_test.go`
- Re-run existing: `internal/relay/availability/credential_fair_lock_test.go`

**Interfaces:**
- No production scheduler change is expected unless this test exposes a real defect.

- [ ] **Step 1: Add a 100-key deterministic fairness test**

Create 100 enabled credentials in one Channel ledger and perform a bounded number of selections. Verify that equal-weight scheduling does not starve any eligible key and that selection counts remain tightly balanced under the existing algorithm.

- [ ] **Step 2: Run the focused test**

Expected: GREEN on current scheduler. If it fails, stop and source-audit the fairness implementation before changing production code.

- [ ] **Step 3: Add a re-entry/revision case if not already covered at this scale**

Temporarily remove/cooldown a subset, reintroduce it, and verify the watermark behavior avoids a catch-up burst.

- [ ] **Step 4: Run availability package tests**

- [ ] **Step 5: Commit test-only coverage**

Commit message: `test(relay): cover 100-key credential fairness`.

---

### Task 7: Full regression and scope gate

**Files:**
- No new production file is required.
- Update this plan with implementation/verification evidence only after code is complete.

- [ ] **Step 1: Run frontend lint/tests/build using the repository's current CI contract**

- [ ] **Step 2: Run backend `go vet ./...` and the full Go test suite using the repository's current CI contract**

- [ ] **Step 3: Confirm scope**

The expected production diff should remain limited to Channel creation UX/preset metadata plus the one-line credential-failover bound change. No DB migration, deploy script, routing-schema change, capability-store redesign, or provider-specific relay fork is allowed.

- [ ] **Step 4: Review credential secrecy**

Confirm no full API key appears in preset analytics, logs, toast summaries, test snapshots, or routing inspector output.

- [ ] **Step 5: Record final CI/PR evidence in this document before merge**

---

## Acceptance Criteria

- NVIDIA and AMD appear as first-class creation presets without new backend Channel types.
- NVIDIA preset uses `https://integrate.api.nvidia.com/v1`; AMD requires an operator-supplied endpoint.
- Presets never ship static model lists.
- 100 NVIDIA keys can be pasted/imported in one operation into the existing Channel key pool.
- Existing equal-weight fairness remains the scheduler; no replacement algorithm is introduced.
- A 100-key test demonstrates no eligible-key starvation under deterministic sequential selection.
- Credential-scoped request failover is bounded at 3 distinct keys, not pool size.
- Channel-level concurrency remains explicit and is not inferred from number of keys.
- No claim is made that multiple keys multiply provider/account quota.
- No database migration, new provider-specific persistence model, or second relay subsystem is introduced.
