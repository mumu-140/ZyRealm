# P1P Passthrough Semantic Hardening + Same-Format Coverage Plan

> **For ChatGPT:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to execute this plan task-by-task. Use superpowers:test-driven-development before runtime changes and superpowers:verification-before-completion before claiming a slice complete.
>
> Status: **ACTIVE IMPLEMENTATION**. Tasks 1–3 (passthrough SSE semantic hardening and invariant verification) are `MERGED + VERIFIED`; Task 4 (OpenAI Chat -> Chat same-format raw passthrough) is `IN PROGRESS`.
>
> Current implementation baseline: `main@9002439d76ec178d539c0ec77d41803761672544` after PR #30. Merged-tree CI run `35037506819` completed successfully.

## Progress ledger

- **Tasks 1–3 — MERGED + VERIFIED** via PR #30, merge `9002439d76ec178d539c0ec77d41803761672544`.
  - RED evidence: CI `35036654522` reproduced the upstream SSE comment/TTFT bug.
  - Final PR-head GREEN: CI `35037353441` passed governance, backend vet/full tests, and frontend lint/test/build.
  - Merged-tree GREEN: CI `35037506819` passed on `main`.
  - Result: raw passthrough now separates transport liveness from semantic payload commitment with a bounded incremental SSE observer; raw bytes remain unchanged for classification.
- **Task 4 — IN PROGRESS** on `codex/p1p-chat-passthrough`, based directly on `main@9002439d...`.
  - Primary acceptance criterion: same-format Chat preserves unknown/future request fields while remaining inside the existing `PassthroughCapable` relay path.
  - Additional fidelity invariant: if the selected upstream model already equals the client top-level `model`, raw request bytes remain byte-identical; if alias rewrite is required, only the top-level `model` may change and nested `model` keys must remain untouched.
  - No new dependency, DB migration, routing schema, replay-policy change, or production operation is allowed in this slice.

## Goal

Harden ZyRealm's existing same-protocol raw HTTP passthrough so that passthrough preserves forward compatibility without weakening first-token timeout, replay safety, routing decisions, credential isolation, or the historical Routing Inspector contract; then extend the existing `PassthroughCapable` architecture to OpenAI Chat Completions before reconsidering broader protocol coverage.

This is **not** a plan to add a second relay path. ZyRealm already has a selective passthrough framework. P1P completes and hardens that framework.

## Why this is next

Current `main` already has:

- `model.PassthroughCapable`, with same-format raw request support;
- Anthropic Messages -> Anthropic Messages HTTP passthrough;
- OpenAI Responses -> OpenAI Responses HTTP passthrough;
- Responses WebSocket passthrough and continuation affinity;
- first-token timeout integrated with dispatch state, replay budget, provider/model failure handling, and downstream commitment;
- P0.1H regression coverage proving that **ZyRealm-generated heartbeat output** is not model payload;
- P1A.1 typed routing-decision persistence and the historical Routing Inspector;
- passthrough SSE semantic hardening from PR #30, so upstream comment/keepalive liveness no longer establishes first-token commitment.

The remaining P1P compatibility gap is OpenAI Chat Completions same-format traffic: it still traverses the explicit `ChatCompletionsRequest` whitelist and JSON rebuild, so unknown/future top-level fields are not guaranteed to survive a same-format relay.

This is a data-plane forward-compatibility gap in an already-shipped architecture, so it takes precedence over speculative capability-schema expansion.

## Architectural contract

P1P must keep one control plane and one routing policy:

```text
client request
    |
    v
parse + route + capability + health + credential eligibility
    |
    v
attempt / dispatch / replay-safety decision
    |
    +--> same-format raw passthrough execution
    |
    +--> standard protocol transform execution
```

Passthrough is only a **data-plane execution mode after normal ZyRealm selection**. It must never bypass:

- provider/channel candidate planning;
- capability rejection and negative-cache behavior;
- credential eligibility, fairness, cooldown, RPM/concurrency, quota, or circuit rules;
- wire/provider attempt budgets;
- dispatch-state and unknown-outcome replay accounting;
- live-request state and manual interrupt;
- typed routing-decision tracing / historical Routing Inspector;
- metrics, audit, or sensitive-header isolation.

### First-token semantic

For P1P, define first-token/stream commitment as:

> the first **substantive provider protocol payload** forwarded downstream, not infrastructure liveness bytes.

The first slice distinguishes SSE comment-only liveness from substantive data-bearing events. It does **not** attempt provider-specific parsing of the “first visible natural-language token”. A completed non-empty `data:` event is sufficient to establish semantic delivery for this stage.

Important consequences:

- ZyRealm's own heartbeat remains non-committing, preserving P0.1H;
- an upstream `: keepalive\n\n` comment may still be forwarded byte-for-byte to the client, but does not stop TTFT or mark downstream semantic commitment;
- if the upstream sends only comments and then stalls past TTFT, the existing first-token timeout path remains eligible to fail over subject to the existing unknown-outcome replay budget;
- once a substantive provider event is forwarded, transparent replay/failover remains forbidden under the existing commitment rules;
- `dispatchMaybeSent` remains an unknown upstream outcome. P1P does not reinterpret a TTFT as “definitely not sent”.

### Raw fidelity semantic

Do not overclaim unconditional byte identity.

Raw passthrough should preserve unknown fields and raw structure whenever ZyRealm has no intentional body mutation. Exact byte identity can legitimately change when ZyRealm must rewrite the selected upstream model or apply an explicit request override. Compression remains a hard gate out of raw passthrough because it mutates the internal request while the original raw body is no longer authoritative.

Forward compatibility — especially survival of unknown future protocol fields — is the primary acceptance criterion for OpenAI Chat passthrough.

For Chat Task 4 specifically:

- when the selected upstream model equals the client top-level `model`, `TransformRequestRaw` should return the original raw body unchanged;
- when model aliasing is required, rewrite only the top-level `model` while preserving all other fields, including unknown fields and nested keys also named `model`;
- request query parameters, `ContentLength`, and `GetBody` must remain correct for existing replay machinery.

## Audited source map

The implementation session must re-read these paths from the then-current `main` before editing:

- `internal/relay/relay_http.go`
- `internal/relay/relay_stream_response.go`
- `internal/relay/stream/processor.go`
- `internal/relay/stream/raw_source.go`
- `internal/relay/first_token_timeout.go`
- `internal/relay/first_token_heartbeat_failover_test.go`
- `internal/relay/relay_http_passthrough_gate_test.go`
- `internal/relay/delivery_commitment_test.go`
- `internal/relay/committed_stream_no_replay_test.go`
- `internal/relay/routing_replay_safety_test.go`
- `internal/relay/routing_decision.go`
- `internal/relay/routing_inspector_metrics_test.go`
- `internal/relay/empty_stream_failover_test.go`
- `internal/transformer/model/interface.go`
- `internal/transformer/outbound/anthropic/messages.go`
- `internal/transformer/outbound/openai/response.go`
- `internal/transformer/outbound/openai/chat.go`
- `internal/helper/param_override.go`
- `scripts/check-governance.sh`
- `.github/workflows/ci.yml`
- `.githooks/pre-push`
- `docs/octopus-development-governance.md`

Reference only, never cherry-pick mechanically:

- `XyzenSun/octopus-customization@dev`
- `internal/relay/passthrough.go`
- `internal/relay/transformers.go`

The reference project demonstrates broader same-format passthrough coverage. ZyRealm must retain its own routing/replay/control-plane architecture.

## Task 1 — RED: prove upstream passthrough comments do not count as first token — ✅ MERGED + VERIFIED

**Files:**

- Create: `internal/relay/passthrough_first_token_semantics_test.go`
- Read/reuse fixtures from: `internal/relay/first_token_heartbeat_failover_test.go`
- Read/reuse one already passthrough-capable protocol from Anthropic Messages or OpenAI Responses.

Write a real-handler regression with two candidates:

- provider A returns HTTP 200 `text/event-stream`, immediately flushes `: keepalive\n\n`, then stalls beyond configured first-token timeout;
- provider B returns a valid same-format streaming response with substantive model payload.

Required behavior is now locked by PR #30 and merged-tree CI `35037506819`.

## Task 2 — GREEN: separate transport writes from semantic stream commitment — ✅ MERGED + VERIFIED

Implemented by PR #30 with:

- `StreamConfig.PayloadObserver` as an optional semantic-delivery hook;
- bounded `SSEPayloadObserver` state across arbitrary raw chunk / CRLF boundaries;
- raw bytes forwarded unchanged;
- comment-only liveness excluded from `OnFirstToken` and semantic commitment;
- completed non-empty `data:` events establishing semantic delivery.

No timeout duration, routing policy, replay budget, or standard transform behavior was changed.

## Task 3 — Lock replay, routing trace, and Inspector invariants — ✅ VERIFIED

The P1P SSE change did not broaden the routing model. Existing repository coverage plus the real-handler passthrough failover regression remained green under full backend CI before merge and on the merged tree. Required invariants remain:

- comment-only pre-semantic timeout can continue only when existing attempt/replay budgets allow it;
- substantive payload remains terminal for transparent replay;
- failure scope/cooldown/circuit classification for first-token timeout is unchanged;
- candidate ordering, credential fairness, and protocol fallback are unchanged;
- persisted routing decisions remain typed and bounded; raw SSE/comment text, headers, bodies, credentials, or free-form provider detail are not added to Inspector records;
- P1A.1's read-only Inspector remains data-plane neutral.

## Task 4 — RED/GREEN: OpenAI Chat -> Chat same-format raw passthrough — 🚧 IN PROGRESS

**Files:**

- Modify: `internal/transformer/outbound/openai/chat.go`
- Add or modify: `internal/transformer/outbound/openai/chat_test.go`
- Create if useful: `internal/transformer/outbound/openai/chat_passthrough_test.go`
- Add relay integration coverage, preferably a focused `internal/relay/relay_http_chat_passthrough_test.go`
- Reuse a small existing raw top-level JSON rewrite helper if appropriate; do not add a dependency just for this slice.

### RED tests first

Lock these contracts before adding `PassthroughCapable` to `ChatOutbound`:

1. a same-format Chat request containing an unknown/future top-level field reaches the upstream with that field intact;
2. when the upstream model equals the request model, the request body remains byte-identical;
3. selected upstream model alias rewrite changes only the top-level `model`; nested objects containing a field named `model` are untouched;
4. query parameters survive;
5. `ContentLength` / `GetBody` allow safe request replay by the existing HTTP machinery;
6. upstream auth remains credential-owned and client `Authorization` cannot replace the selected upstream credential;
7. allowed client headers continue through the existing header-copy/template policy rather than creating a second header policy;
8. streaming raw response bytes remain passthrough while the Task 2 semantic observer controls first-token commitment;
9. non-stream same-format Chat uses raw request forwarding without bypassing normal response/error/routing metrics;
10. cross-format routes (for example Chat -> Responses or Chat -> Anthropic) remain on the standard transformer path;
11. compression still disables raw-body authority and therefore raw passthrough;
12. explicit parameter override remains an intentional body mutation. Tests should require semantic override behavior and unknown-field survival, not impossible byte identity after an override.

### GREEN implementation

Implement the existing optional interface on `ChatOutbound` rather than creating a second relay subsystem:

- `CanPassthrough(inboundFormat)` only for OpenAI Chat same-format traffic;
- `TransformRequestRaw(...)` that preserves the raw body except for the minimum required upstream-model rewrite;
- protocol-appropriate `PassthroughConfig()`;
- no change to `ChatCompletionsRequest` standard-path whitelist, because cross-format normalization still needs a controlled schema.

Prefer existing ZyRealm code over importing `tidwall/sjson` solely because the reference project uses it. Any new dependency requires a separate justification and is out of scope by default.

## Task 5 — Audit-only successor coverage: Embeddings and Images

Do **not** mechanically copy the reference project's endpoint matrix.

After Chat passthrough is green, audit:

- OpenAI embedding inbound/outbound paths;
- `internal/relay/images.go`;
- `internal/relay/images_request.go`;
- `internal/relay/images_proxy.go`;
- image multipart/body handling, metrics, limits, and proxy semantics.

Record which endpoints can safely adopt the same framework. Images in particular have a separate relay surface and may require multipart-aware semantics. Unless the audit proves the change is trivial and isolated, schedule embedding/image passthrough as a successor slice rather than expanding the first P1P implementation PR.

## Task 6 — Validation gates using the repository's existing scripts

Do not invent a parallel validation workflow and do not run build/test work on the local Mac.

### Governance gate

The existing branch must remain under an accepted namespace (`codex/...` for this plan). Use the repository's own governance entrypoint:

```bash
bash scripts/check-governance.sh --repo
```

### Targeted backend gates

Run on GitHub CI or the approved fixed-version remote environment, not the local workstation:

```bash
go test -buildvcs=false ./internal/relay -run 'TestHandlerFirstTokenTimeoutFailsOverAfterEarlyHeartbeat|Test.*Passthrough.*FirstToken|Test.*Chat.*Passthrough'
go test -buildvcs=false ./internal/relay/stream
go test -buildvcs=false ./internal/transformer/outbound/openai
go test -buildvcs=false ./internal/transformer/outbound/anthropic
```

Adjust exact test regexes to the final names; do not skip the corresponding packages.

### Full repository gate

Use the existing CI workflow as authority:

- governance: `bash scripts/check-governance.sh --repo`;
- backend: repository CI must pass `go vet ./...` and `go test -buildvcs=false ./...`;
- frontend: existing pnpm install/lint/test/build jobs must remain green even if this slice has no frontend changes.

Before merge:

- pin evidence to the exact PR head SHA;
- inspect changed filenames and confirm no DB migration, deploy/compose, dependency, unrelated frontend, or production-state change slipped in;
- inspect all review threads and resolve or explicitly disposition them;
- require full PR-head CI green;
- after merge, require merged-tree CI green before marking P1P complete;
- no production deployment is part of this plan unless separately requested.

## Task 7 — Closure record and roadmap promotion

Only after implementation is merged and merged-tree CI is green:

- update `docs/plans/2026-09-14-octopus-gap-roadmap.md` from P1P `NEXT` to `MERGED + VERIFIED`;
- record RED and GREEN runs, final PR head SHA, merge SHA, merged-tree CI, changed-file scope, and review-thread audit;
- create the next source-audit plan from the new `main`; do not reuse this file's source assumptions after `main` moves.

## Queued after P1P, not part of this implementation

### P1C — Control-plane conveniences

Source-audit only after P1P is closed:

1. **tri-state parameter/header override**: absent = inherit, `null` = delete, while `0`, `false`, `""`, `[]`, and `{}` remain explicit values. Preserve ZyRealm credential-owned header protection; do not infer that users may delete/replace upstream authorization merely because the reference fork permits it;
2. **privileged direct-route diagnostic**: an authenticated operator/debug path that can bypass candidate ranking for diagnosis, but must still pass credential eligibility, quota/RPM/concurrency, circuit/health safety where applicable, request lifecycle, interrupt, tracing, and audit. Do not overload the public model string as `channel/model`.

Generic HTTP sticky routing is intentionally deferred. ZyRealm already has protocol-semantic Responses continuation affinity, and binding a generic session directly to a credential can fight the existing credential scheduler.

## Explicit non-goals

- No second passthrough subsystem.
- No rewrite of first-token timeout policy or timeout duration.
- No expansion of replay allowance, wire/provider attempt budgets, or failure scopes.
- No replacement of adaptive routing, credential fairness, circuit breaking, cooldown, or protocol fallback.
- No DB migration or capability schema.
- No generic HTTP sticky-session feature in P1P.
- No direct-channel route in P1P.
- No upstream TOTP authentication work; the reference fork's TOTP code is account 2FA, not provider authentication.
- No dependency addition by default.
- No production deployment.

## Completion definition

P1P is complete only when all of the following are true:

- upstream passthrough SSE comment/keepalive cannot satisfy first-token or semantic commitment by itself;
- raw passthrough bytes remain unmodified for classification purposes;
- existing P0.1H local-heartbeat failover remains green;
- existing unknown-outcome replay safety and post-payload no-replay semantics remain green;
- OpenAI Chat same-format passthrough preserves unknown future fields and uses the existing `PassthroughCapable` framework;
- cross-format transformations remain unchanged;
- routing traces / historical Inspector remain typed, bounded, non-sensitive, and data-plane neutral;
- governance, targeted backend suites, full Go tests, and existing frontend CI are green on the exact PR head;
- merged-tree CI is green;
- no unrelated runtime/schema/deploy/dependency changes are present.
