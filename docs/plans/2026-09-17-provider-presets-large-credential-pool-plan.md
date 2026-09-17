# Provider Presets + Large Credential Pool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add lightweight prebuilt Channel presets for NVIDIA, AMD and other OpenAI-compatible providers, while making large credential pools such as ~100 NVIDIA API keys practical without replacing ZyRealm's current credential scheduler.

**Architecture:** Presets are control-plane templates only. They pre-fill the existing Channel create form (`name`, protocol `type`, `base_urls`, model-discovery behavior and optional concurrency hint), then use the existing Channel API and relay runtime unchanged. Large key pools continue to use current credential fairness, cooldown, circuit breaking and provider failover.

**Tech Stack:** Existing Next.js/React/TypeScript Channel UI + existing Go relay runtime.

**Baseline:** current `main` already has provider-local fair credential scheduling, credential cooldown, circuit breaking, credential revision handling, Channel-level concurrency/RPM gates, and bounded request-local credential failover.

## Global constraints

- No new Provider backend domain or database migration.
- No new Channel protocol enum for NVIDIA/AMD/OpenRouter-style OpenAI-compatible providers.
- Do not replace `availability.SelectCredentialFair` or existing routing/failure logic.
- Do not hard-code provider model lists; continue using current model discovery/refresh flow.
- Preset base URLs remain editable after prefill.
- Multiple API keys must never be described as multiplying account/project quota.
- A single request must never sweep the full key pool.
- No local Mac build/deploy; use repository CI/remote execution policy.

## First preset catalog

1. **NVIDIA NIM**
   - Protocol: `ChannelType.OpenAIChat`
   - Base URL: `https://integrate.api.nvidia.com/v1`
   - API key required
   - Dynamic model discovery; no static model list

2. **AMD Radeon Cloud**
   - Protocol: `ChannelType.OpenAIChat`
   - Base URL: `https://developer.amd.com.cn/radeon/api/v1`
   - API key required
   - Dynamic model discovery where upstream supports it
   - This is the fixed hosted Radeon Cloud preset discussed for this slice; self-hosted AMD AIM/ROCm remains a separate custom OpenAI-compatible use case.

3. **OpenRouter** — OpenAI-compatible preset
4. **Groq** — OpenAI-compatible preset
5. **Cerebras** — OpenAI-compatible preset
6. **Together AI** — OpenAI-compatible preset
7. **Fireworks AI** — OpenAI-compatible preset
8. **DeepInfra** — OpenAI-compatible preset
9. **SiliconFlow** — OpenAI-compatible preset
10. **Custom OpenAI-compatible** — blank editable base URL

Provider URLs other than NVIDIA/AMD must be source-audited immediately before implementation; omit any uncertain preset rather than guessing.

---

## Task 1 — Typed provider preset registry

**Files**
- Create `web/src/components/modules/channel/provider-presets.ts`
- Test `web/src/components/modules/channel/provider-presets.test.ts`

Use a small UI-only type:

```ts
export type ProviderPreset = {
  id: string;
  name: string;
  type: ChannelType;
  baseUrl: string;
  apiKeyRequired: boolean;
  modelDiscovery: 'openai' | 'manual';
  recommendedMaxConcurrency?: number;
};
```

Required tests:

```ts
expect(getProviderPreset('nvidia')).toMatchObject({
  type: ChannelType.OpenAIChat,
  baseUrl: 'https://integrate.api.nvidia.com/v1',
});

expect(getProviderPreset('amd-radeon-cloud')).toMatchObject({
  type: ChannelType.OpenAIChat,
  baseUrl: 'https://developer.amd.com.cn/radeon/api/v1',
});
```

Also assert presets contain no static `models` array.

---

## Task 2 — Preset picker in Channel Create only

**Files**
- Create `web/src/components/modules/channel/ProviderPresetPicker.tsx`
- Modify `web/src/components/modules/channel/Create.tsx`
- Test `web/src/components/modules/channel/ProviderPresetPicker.test.tsx`

Behavior:

```text
Create Channel
  -> choose preset
  -> prefill existing ChannelFormData
  -> user enters/imports API keys
  -> refresh models through existing flow
  -> save through existing useCreateChannel()
```

Do not create a separate provider-specific submit path. Editing an existing Channel is unchanged in this slice.

---

## Task 3 — Bulk key import

**Files**
- Create `web/src/components/modules/channel/key-import.ts`
- Create `web/src/components/modules/channel/KeyBulkImport.tsx`
- Modify `web/src/components/modules/channel/Form.tsx`
- Tests for parser and UI

Input contract: one API key per line. Trim whitespace, ignore blank lines, preserve first-seen order, de-duplicate exact values, and convert directly to the existing `ChannelFormData.keys` structure.

Example UX:

```text
Bulk import keys
[ one key per line ]

100 valid / 2 duplicates / 3 blank lines ignored
[Import]
```

Never display full imported credentials in summaries, logs, toasts or snapshots.

---

## Task 4 — Large NVIDIA key-pool concurrency guidance

Current Channel creation defaults to `max_concurrency = 3`; this gate is applied at the Channel level before credential selection. Therefore 100 keys can be scheduled fairly but still be locally capped at three concurrent requests unless the operator raises the Channel limit.

Required behavior:

- Do **not** derive concurrency automatically from key count.
- For large imported pools, show a non-blocking hint that Channel-level `max_concurrency` remains the local admission cap.
- A preset may expose a recommended starting value, but it must remain editable and must not imply upstream quota.

Test example: importing 100 keys while `max_concurrency = 3` surfaces the hint but does not block save.

---

## Task 5 — Bounded credential failover: 2 -> 3

**Files**
- Modify `internal/relay/credential_rotation.go`
- Extend `internal/relay/credential_provider_failover_test.go`
- Re-run credential fairness/cooldown integration tests

Target:

```go
const defaultMaxCredentialsPerProvider = 3
```

Semantics:

```text
credential A -> credential-scoped failure -> cooldown/exclude
credential B -> credential-scoped failure -> cooldown/exclude
credential C -> success
credential D -> untouched
```

After three distinct credential failures, skip the provider using existing failover/replay rules. Never make this limit proportional to pool size.

---

## Task 6 — 100-key fairness regression coverage

**Files**
- Extend `internal/relay/availability/credential_fair_test.go`
- Re-run `credential_fair_lock_test.go`

Create 100 enabled credentials in one Channel ledger and perform deterministic selections. Verify:

- no eligible credential is starved;
- selection counts remain tightly balanced;
- temporarily unavailable/reintroduced credentials re-enter at the current watermark rather than causing a catch-up burst;
- no production scheduler rewrite is made unless this test exposes an actual defect.

---

## Task 7 — Full regression and scope gate

Before merge:

- frontend lint/tests/build all green;
- `go vet ./...` and full Go tests green;
- no DB migration;
- no deploy/compose change;
- no routing-schema/capability-store redesign;
- no provider-specific relay fork;
- no API key leakage in logs, UI summaries, tests or Routing Inspector.

## Acceptance criteria

- NVIDIA and AMD Radeon Cloud are first-class Channel creation presets.
- NVIDIA uses `https://integrate.api.nvidia.com/v1`.
- AMD Radeon Cloud uses `https://developer.amd.com.cn/radeon/api/v1`.
- Presets contain no static model inventories.
- ~100 NVIDIA keys can be imported in one operation into the existing Channel key pool.
- Existing equal-weight credential fairness remains authoritative.
- 100-key regression coverage demonstrates no starvation.
- Request-local credential failover is bounded at 3 distinct credentials.
- Channel concurrency remains explicit and independent of number of credentials.
- No claim is made that 100 keys equal 100x quota.
