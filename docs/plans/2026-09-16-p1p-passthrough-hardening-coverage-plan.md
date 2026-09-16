# P1P Passthrough Semantic Hardening + Same-Format Coverage Plan

> **For ChatGPT:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to execute this plan task-by-task. Use superpowers:test-driven-development before runtime changes and superpowers:verification-before-completion before claiming a slice complete.
>
> Status: **MERGED + VERIFIED**. Tasks 1–4 are merged and verified on `main`; the P1P runtime slice is closed. Embeddings/Images coverage is a successor source-audit slice and is not part of P1P completion.
>
> Current verified runtime baseline: `main@5b773f13f84734eb222ad99e35b5840c69d2f188` after PR #31. Merged-tree CI run `35051237424` passed governance, backend Vet/full tests, and frontend lint/test/build.

## Progress ledger

- **Tasks 1–3 — MERGED + VERIFIED** via PR #30, merge `9002439d76ec178d539c0ec77d41803761672544`.
  - RED evidence: CI `35036654522` reproduced the upstream SSE comment/TTFT bug.
  - Final PR-head GREEN: CI `35037353441` passed governance, backend Vet/full tests, and frontend lint/test/build.
  - Merged-tree GREEN: CI `35037506819` passed on `main`.
  - Result: raw passthrough now separates transport liveness from semantic payload commitment with a bounded incremental SSE observer; raw bytes remain unchanged for classification.
- **Task 4 — MERGED + VERIFIED** via PR #31, branch `codex/p1p-chat-passthrough`, based directly on `main@9002439d76ec178d539c0ec77d41803761672544`.
  - Initial RED: CI `35047865171` proved `ChatOutbound` did not yet implement `model.PassthroughCapable` while existing coverage remained green.
  - First GREEN: CI `35048039449` passed governance, backend Vet/full tests, and frontend lint/test/build after the initial implementation.
  - Duplicate-model RED: CI `35048544996` proved a first-match-only model rewrite left a later conflicting top-level `model` value (`ambiguous-last`) on the wire.
  - Final PR-head GREEN: CI `35048893875` passed on exact head `130a4c71ee5ea1140a58d7989e1fafd2421e15aa`.
  - Scope audit: exactly four changed files — this plan, one relay acceptance test, one Chat passthrough implementation file, and one adapter test file; no DB migration, dependency, routing schema, timeout/replay-policy, frontend runtime, deploy/compose, or production-state change.
  - Review audit: no inline review threads and no submitted reviews at the final head.
  - Squash merge: `5b773f13f84734eb222ad99e35b5840c69d2f188`.
  - Merged-tree GREEN: CI `35051237424` passed governance, backend Vet/full tests, and frontend lint/test/build on the exact merge commit.
  - Result: OpenAI Chat -> Chat same-format traffic now uses the existing `PassthroughCapable` execution path; unknown/future request and response fields survive; a single matching top-level `model` preserves raw request bytes exactly; alias rewrites affect only top-level `model` values; duplicate top-level model keys are normalized to the control-plane-selected upstream model; credential isolation, query parameters, `ContentLength`, `GetBody`, routing, replay, metrics, and Inspector behavior remain on the existing ZyRealm control plane.

## Goal

Harden ZyRealm's existing same-protocol raw HTTP passthrough so that passthrough preserves forward compatibility without weakening first-token timeout, replay safety, routing decisions, credential isolation, or the historical Routing Inspector contract; then extend the existing `PassthroughCapable` architecture to OpenAI Chat Completions before reconsidering broader protocol coverage.

This is **not** a plan to add a second relay path. ZyRealm already has a selective passthrough framework. P1P completes and hardens that framework.

## Completion state

P1P is closed on `main@5b773f13f84734eb222ad99e35b5840c69d2f188`.

The two source-audited gaps that motivated this plan are now closed:

1. raw passthrough streaming no longer treats upstream SSE comment/keepalive liveness as semantic first-token commitment;
2. OpenAI Chat Completions same-format traffic no longer has to rebuild the request through the explicit `ChatCompletionsRequest` whitelist, so unknown/future fields can survive the relay.

Embeddings and Images remain intentionally outside this closure. They require a fresh source audit against the post-P1P main baseline rather than automatic extension of the Chat implementation.

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

- when there is one unambiguous top-level `model` and it already equals the selected upstream model, `TransformRequestRaw` returns the original raw body unchanged;
- when model aliasing is required, only top-level `model` values are rewritten while preserving all other fields, including unknown fields and nested keys also named `model`;
- duplicate top-level `model` keys are normalized to the selected upstream model to prevent parser-dependent wire/control-plane divergence;
- request query parameters, `ContentLength`, and `GetBody` remain correct for existing replay machinery.

## Audited source map

The implementation session re-read these paths from the then-current `main` before editing:

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

The reference project demonstrates broader same-format passthrough coverage. ZyRealm retained its own routing/replay/control-plane architecture.

## Task 1 — RED: prove upstream passthrough comments do not count as first token — ✅ MERGED + VERIFIED

**Files:**

- Create: `internal/relay/passthrough_first_token_semantics_test.go`
- Read/reuse fixtures from: `internal/relay/first_token_heartbeat_failover_test.go`
- Read/reuse one already passthrough-capable protocol from Anthropic Messages or OpenAI Responses.

Write a real-handler regression with two candidates:

- provider A returns HTTP 200 `text/event-stream`, immediately flushes `: keepalive\n\n`, then stalls beyond configured first-token timeout;
- provider B returns a valid same-format streaming response with substantive model payload.

Required behavior is locked by PR #30 and merged-tree CI `35037506819`.

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

## Task 4 — RED/GREEN: OpenAI Chat -> Chat same-format raw passthrough — ✅ MERGED + VERIFIED

**Files:**

- Created: `internal/transformer/outbound/openai/chat_passthrough.go`
- Created: `internal/transformer/outbound/openai/chat_passthrough_test.go`
- Created: `internal/relay/relay_http_chat_passthrough_test.go`
- Updated: this plan.

### Locked contracts

1. a same-format Chat request containing an unknown/future top-level field reaches the upstream with that field intact;
2. when there is one unambiguous top-level model and it equals the upstream model, the request body remains byte-identical;
3. selected upstream model alias rewrite changes only top-level `model` values; nested objects containing a field named `model` are untouched;
4. duplicate top-level `model` keys are normalized to the selected upstream model;
5. query parameters survive;
6. `ContentLength` / `GetBody` allow safe request replay by the existing HTTP machinery;
7. upstream auth remains credential-owned and client `Authorization` cannot replace the selected upstream credential;
8. allowed client headers continue through the existing header-copy/template policy rather than creating a second header policy;
9. streaming raw response bytes remain passthrough while the Task 2 semantic observer controls first-token commitment;
10. non-stream same-format Chat uses raw request forwarding without bypassing normal response/error/routing metrics;
11. cross-format routes remain on the standard transformer path;
12. compression still disables raw-body authority and therefore raw passthrough;
13. explicit parameter override remains an intentional body mutation rather than an impossible byte-identity guarantee after override.

### Implemented path

`ChatOutbound` implements the existing optional interface rather than creating a second relay subsystem:

- `CanPassthrough(inboundFormat)` only accepts OpenAI Chat same-format traffic;
- `TransformRequestRaw(...)` preserves the raw body unless a top-level model rewrite is required;
- `PassthroughConfig()` keeps metrics collection on the existing sidecar parser;
- the standard `ChatCompletionsRequest` whitelist remains unchanged for cross-format normalization;
- no new dependency was added.

### Completion evidence

- initial RED: `35047865171`;
- first GREEN: `35048039449`;
- duplicate-model RED: `35048544996`;
- final exact-head GREEN: `35048893875` on `130a4c71ee5ea1140a58d7989e1fafd2421e15aa`;
- PR: `#31 feat(openai): add Chat same-format raw passthrough`;
- squash merge: `5b773f13f84734eb222ad99e35b5840c69d2f188`;
- merged-tree CI: `35051237424`, all governance/backend/frontend jobs successful;
- no production deployment was performed.

## Task 5 — Audit-only successor coverage: Embeddings and Images — NEXT SUCCESSOR AUDIT

This audit is intentionally **not required for P1P closure**.

Do **not** mechanically copy the reference project's endpoint matrix. Start from post-P1P `main@5b773f13f84734eb222ad99e35b5840c69d2f188` and re-audit:

- OpenAI embedding inbound/outbound paths;
- `internal/relay/images.go`;
- `internal/relay/images_request.go`;
- `internal/relay/images_proxy.go`;
- image multipart/body handling, metrics, limits, and proxy semantics.

Record which endpoints can safely adopt the same framework. Images in particular have a separate relay surface and may require multipart-aware semantics. Unless the audit proves the change is trivial and isolated, schedule embedding/image passthrough as a successor slice rather than expanding P1P retroactively.

## Task 6 — Validation gates using the repository's existing scripts — ✅ VERIFIED

No parallel validation workflow was created and no build/test work was run on the local Mac.

### Governance gate

The implementation branches remained under accepted `codex/...` namespaces and the repository governance job passed on both implementation PR heads and merged trees.

### Backend and full repository gates

The repository CI contract remained authoritative:

- governance: `bash scripts/check-governance.sh --repo`;
- backend: `go vet ./...` and `go test -buildvcs=false ./...`;
- frontend: existing pnpm install/lint/test/build jobs.

Final Task 4 exact-head evidence is CI `35048893875`; final merged-tree evidence is CI `35051237424`.

## Task 7 — Closure record and roadmap promotion — ✅ COMPLETE

Closure evidence:

- PR #30 closed Tasks 1–3 and merged as `9002439d76ec178d539c0ec77d41803761672544`; merged-tree CI `35037506819` was green.
- PR #31 closed Task 4; final head `130a4c71ee5ea1140a58d7989e1fafd2421e15aa`, final PR-head CI `35048893875`, squash merge `5b773f13f84734eb222ad99e35b5840c69d2f188`, merged-tree CI `35051237424` all green.
- PR #31 changed exactly four files and introduced no DB migration, dependency, routing schema, replay-policy, frontend runtime, deploy/compose, or production-state change.
- Review-thread audit found no inline review threads and no submitted reviews at the final PR head.
- `docs/plans/2026-09-14-octopus-gap-roadmap.md` is promoted from P1P `NEXT` to `MERGED + VERIFIED` in the docs-only closeout slice.
- Any successor source audit must start from the then-current `main`, with `5b773f13f84734eb222ad99e35b5840c69d2f188` as the verified post-P1P runtime baseline; this plan's implementation-time source assumptions must not be reused blindly.

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
- governance, full Go tests, and existing frontend CI are green on the exact implementation PR heads;
- merged-tree CI is green;
- no unrelated runtime/schema/deploy/dependency changes are present.

All P1P completion conditions were satisfied by PR #30 + PR #31 and their merged-tree CI runs, with `main@5b773f13f84734eb222ad99e35b5840c69d2f188` as the verified runtime closure baseline.