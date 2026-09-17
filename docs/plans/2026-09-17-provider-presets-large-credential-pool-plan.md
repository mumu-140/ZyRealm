# Provider Presets + Large Credential Pool Implementation Plan

> **Status:** IMPLEMENTED + CODE-VERIFIED on PR #35. Production-code head `bad7dba4fee5199ce2d63664f9538b0f0f6402ac` passed repository CI `35182514341` (governance, backend Vet/full tests, frontend lint/tests/build). This document update is evidence-only and does not deploy or change production state.
>
> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement or extend this plan task-by-task.

**Goal:** Add lightweight prebuilt Channel presets for selected OpenAI-compatible providers and make large credential pools such as ~100 NVIDIA API keys practical without replacing ZyRealm's credential scheduler.

**Architecture:** Presets are frontend control-plane templates only. They pre-fill the existing Channel create form (`name`, protocol `type`, `base_urls`) and then use the existing Channel API, model discovery, key persistence, and relay runtime. Large key pools continue to use current credential fairness, cooldown, circuit breaking, failure classification, replay safety, and provider failover.

**Tech Stack:** Existing Next.js/React/TypeScript Channel UI + existing Go relay runtime.

**Baseline:** implementation started from `main@1b6efce6aa2c618a53b8da441180ef8e5e98c054`.

## Global constraints

- No new Provider backend domain or database migration.
- No new Channel protocol enum for NVIDIA/AMD/OpenRouter-style OpenAI-compatible providers.
- Do not replace `availability.SelectCredentialFair` or existing routing/failure logic.
- Do not hard-code provider model lists; continue using current model discovery/refresh flow.
- Preset base URLs remain editable after prefill.
- Multiple API keys must never be described as multiplying account/project/model quota.
- A single request must never sweep the full key pool.
- No local Mac build/deploy; use repository CI/remote execution policy.

## Implemented preset catalog

The first shipped registry deliberately contains only endpoints verified for this slice:

1. **NVIDIA NIM**
   - Protocol: `ChannelType.OpenAIChat`
   - Base URL: `https://integrate.api.nvidia.com/v1`
   - API key required
   - Existing OpenAI-compatible model discovery; no static model list

2. **AMD Radeon Cloud**
   - Protocol: `ChannelType.OpenAIChat`
   - Base URL: `https://developer.amd.com.cn/radeon/api/v1`
   - API key required
   - Existing OpenAI-compatible model discovery
   - Self-hosted AMD AIM/ROCm remains available through the custom OpenAI-compatible preset rather than receiving an invented universal hosted URL.

3. **OpenRouter**
   - Protocol: `ChannelType.OpenAIChat`
   - Base URL: `https://openrouter.ai/api/v1`

4. **Groq**
   - Protocol: `ChannelType.OpenAIChat`
   - Base URL: `https://api.groq.com/openai/v1`

5. **SiliconFlow**
   - Protocol: `ChannelType.OpenAIChat`
   - Base URL: `https://api.siliconflow.cn/v1`

6. **Custom OpenAI-compatible**
   - Protocol: `ChannelType.OpenAIChat`
   - Blank editable base URL

**Deferred:** Cerebras, Together AI, Fireworks AI, and DeepInfra were intentionally not hard-coded in this slice because their exact first-party endpoint metadata was not locked during implementation. Add them only after a fresh official-source audit.

---

## Task 1 — Typed provider preset registry — COMPLETE

**Files**
- Created `web/src/components/modules/channel/provider-presets.ts`
- Added source-contract coverage in `web/tests/provider-presets.test.mjs`

The actual repository frontend test harness is `node --experimental-strip-types --test tests/*.test.mjs`; the earlier `.test.ts/.tsx` assumption was corrected during source audit and no new frontend test dependency was introduced.

Implemented registry metadata remains UI-only:

```ts
export type ProviderPreset = {
    id: ProviderPresetID;
    name: string;
    type: ChannelType;
    baseUrl: string;
    apiKeyRequired: boolean;
    modelDiscovery: 'openai' | 'manual';
};
```

No preset contains a static `models` field.

**TDD evidence**
- RED head `9fdcd74dbc2e8e25f394bcb741890f1466cde795`, CI `35181903909`: only the two new preset-contract tests failed because the registry did not yet exist; existing frontend tests passed.
- GREEN head `088955993e371af58ee7b1e49f7e0ef19cea986e`, CI `35181989614`: governance, backend Vet/full tests, frontend lint/tests/build all success.

---

## Task 2 — Preset picker in Channel Create only — COMPLETE

**Files**
- Created `web/src/components/modules/channel/ProviderPresetPicker.tsx`
- Modified `web/src/components/modules/channel/Create.tsx`
- Added `web/tests/provider-preset-create.test.mjs`

Behavior:

```text
Create Channel
  -> choose preset
  -> prefill existing ChannelFormData
  -> clear any previously entered provider credential/model state
  -> user enters/imports API keys
  -> refresh models through existing ChannelForm flow
  -> save through existing useCreateChannel()
```

There is no provider-specific submit API and no edit-mode preset migration. Switching presets resets the key list to one blank row so a credential entered for one provider cannot silently be carried into another provider.

---

## Task 3 — Bulk key import — COMPLETE

**Files**
- Created `web/src/components/modules/channel/key-import.ts`
- Created `web/src/components/modules/channel/KeyBulkImport.tsx`
- Integrated the helper in `web/src/components/modules/channel/Create.tsx`
- Added `web/tests/channel-key-import.test.mjs`

The large `Form.tsx` did not need modification: bulk import is a create-flow helper that writes directly into the existing `ChannelFormData.keys` array before the normal `ChannelForm` renders/persists it.

Input contract:
- one API key per line;
- trim whitespace;
- ignore blank lines;
- preserve first-seen order;
- de-duplicate exact credential values;
- merge with already-entered non-empty keys;
- never echo full credentials in summary/toast text.

Coverage includes an exact 100-line import.

---

## Task 4 — Large key-pool concurrency guidance — COMPLETE

Current Channel creation still defaults to `max_concurrency = 3`. No concurrency is derived from key count.

For a large imported credential pool, the UI surfaces a non-blocking warning when Channel concurrency remains very low. The warning explicitly states that Channel concurrency is the local cap and key count does not imply provider quota.

**Frontend RED/GREEN evidence for Tasks 2–4**
- RED head `9c1084596abfb48f64b02b10c73bbaad4dc8bda3`, CI `35182128442`: all existing frontend tests passed; the five new create/import assertions failed because `ProviderPresetPicker` / `key-import.ts` did not yet exist.
- GREEN head `328ff55bdd1538bb525e8a5c99812ed37d85f1fd`, CI `35182231101`: governance, backend Vet/full tests, frontend lint/tests/build all success.

---

## Task 5 — Bounded credential failover: 2 -> 3 — COMPLETE

**Files**
- Modified `internal/relay/credential_rotation.go`
- Extended `internal/relay/credential_provider_failover_test.go`
- Extended `internal/relay/credential_rotation_test.go`

Production change is intentionally one policy constant:

```go
const defaultMaxCredentialsPerProvider = 3
```

Verified semantics:

```text
A -> credential-scoped failure -> cooldown/exclude
B -> credential-scoped failure -> cooldown/exclude
C -> success
D -> untouched
```

A second integration path verifies that after A/B/C all fail with credential-scoped failures, the fourth key remains untouched and the existing provider failover proceeds to the next Channel/provider.

No failure classifier, timeout, replay allowance, circuit rule, cooldown algorithm, or scheduler was changed.

**TDD evidence**
- RED head `5d68447dffb04577120d052c2a13f19a400ba2b6`, CI `35182351382`: backend failed only on the new required behavior. Logs showed `key1=1 key2=1 key3=0` for the budget test and a terminal 401 after two attempts for the third-key recovery test.
- GREEN implementation head `9ff7c1c63fd0f4c84d79f341cb5798ba3b5a7a48`, CI `35182465075`: backend Vet/full tests succeeded with the limit set to 3.

---

## Task 6 — 100-key fairness regression coverage — COMPLETE

**File**
- Extended `internal/relay/availability/credential_fair_test.go`

The test creates 100 enabled credentials in one provider ledger and performs 1000 selections. Every key must be selected exactly 10 times. Existing tests continue to cover provider-local ledgers, new-member/revision watermarking, temporary-ineligibility re-entry, preferred-key charging, continuous-allocation protection, and concurrent atomicity.

No production fairness code changed. This confirms that ~100-key equal-weight scheduling is already handled by the existing scheduler and does not require a heap/ring/new provider subsystem.

**Verification evidence**
- Code head `bad7dba4fee5199ce2d63664f9538b0f0f6402ac`, CI `35182514341`: governance, backend `go vet ./...` + full Go tests, frontend lint/tests/build all success.

---

## Task 7 — Full regression and scope gate — COMPLETE FOR CODE HEAD

Verified on code head `bad7dba4fee5199ce2d63664f9538b0f0f6402ac`:

- [x] governance green;
- [x] frontend lint green;
- [x] frontend tests green;
- [x] frontend production build green;
- [x] `go vet ./...` green;
- [x] full Go tests green;
- [x] no DB migration;
- [x] no deploy/compose or production-state change;
- [x] no new Channel protocol enum;
- [x] no routing-schema/capability-store redesign;
- [x] no provider-specific relay fork;
- [x] no static provider model inventories;
- [x] no full credential values surfaced in import summaries/toasts;
- [x] credential fairness/cooldown/circuit/replay architecture preserved.

## Acceptance criteria — SATISFIED

- [x] NVIDIA and AMD Radeon Cloud are first-class Channel creation presets.
- [x] NVIDIA uses `https://integrate.api.nvidia.com/v1`.
- [x] AMD Radeon Cloud uses `https://developer.amd.com.cn/radeon/api/v1`.
- [x] Presets contain no static model inventories.
- [x] ~100 NVIDIA keys can be imported in one operation into the existing Channel key pool.
- [x] Existing equal-weight credential fairness remains authoritative.
- [x] 100-key regression coverage demonstrates no starvation and exact equal distribution in the deterministic case.
- [x] Request-local credential failover is bounded at 3 distinct credentials, independent of pool size.
- [x] Channel concurrency remains explicit and independent of number of credentials.
- [x] No claim is made that 100 keys equal 100x quota.
- [x] No DB migration, new Provider persistence model, scheduler rewrite, or second relay subsystem was introduced.

## Merge boundary

PR #35 remains the implementation boundary. This slice is ready for final diff/review-thread verification after the evidence-only document update. Merge and deployment are separate explicit actions; neither is implied by this plan.
