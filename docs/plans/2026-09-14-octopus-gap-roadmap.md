# Octopus Gap Adoption Roadmap

> Status: staged adoption index. P0.1 is implemented and CI-verified on its feature branch, but is not merged yet; later slices remain intentionally unplanned.
>
> Baseline: ZyRealm `main` at `cbe4638b01aa5beb1a46f73dfb41cabaecaf890c` (2026-09-14).

## Planning rule

Adopt upstream ideas one slice at a time.

1. Deep-read the ZyRealm source paths touched by the current slice.
2. Write a detailed implementation plan for that slice only.
3. Implement it, run the full relevant regression suite, and merge it cleanly.
4. Only then deep-read and plan the next slice.
5. Do **not** lock P1 schema or architecture while P0 is still moving.

This is deliberate: Octopus and ZyRealm now have materially different runtime architectures. We should transfer useful ideas, not copy upstream patches mechanically.

## P0 sequence

### P0.1 — Client-header templates — IMPLEMENTED + VERIFIED, AWAITING MERGE

Allow channel custom-header **values** to explicitly reference safe metadata from the original client request, using syntax such as:

```text
X-Upstream-Project: {client_header:OpenAI-Project}
X-Tenant-Context: tenant-{client_header:X-Tenant-ID}
```

Upstream reference: Octopus commit `1c48ee5105042b8eebaba05c05b2773b04e6c7f3` (`client_header`).

ZyRealm-specific implementation keeps template metadata structurally separate from request bodies and ordinary WebSocket header forwarding. It uses a sanitized request-scoped template source, rejects credential/cookie/proxy/forwarded/WebSocket-control sources, validates configuration before persistence, renders per request/attempt without mutating cached channels, and includes final rendered headers in WebSocket pool identity.

Detailed source-audited plan: [`2026-09-14-client-header-template-plan.md`](./2026-09-14-client-header-template-plan.md).

Verification evidence before this roadmap-only status update:

- feature branch: `codex/client-header-template`;
- verified implementation head: `dd27764924611ecef6c21aeece4085f9951a1924`;
- GitHub Actions CI run: `34830206634`;
- governance: success;
- backend: `go vet ./...` success and `go test -buildvcs=false ./...` success;
- frontend: lint, tests, and production build success;
- regression coverage includes HTTP rendering, sensitive-source rejection, persistence validation, WebSocket ordinary-header isolation, final-header pool separation, and a JSON-looking header value that is proven not to enter the WebSocket `response.create` JSON payload.

**Gate before the next implementation slice:** merge/stabilize P0.1 first.

### P0.1H — First-token timeout must fail over — QUEUED HOTFIX, NOT YET SOURCE-AUDITED

Observed current terminal outcome:

```text
channel failed: failed to send request: first token timeout (30s)

failed to send request: first token timeout (30s)
```

Required behavior: a **first-token timeout on one attempt must not directly terminate the whole request** while another eligible route/candidate exists. It should enter the same retryable failover machinery used by other retryable attempt failures and advance to the next eligible credential/provider/channel/candidate according to ZyRealm's existing routing policy.

The overall request may become failed only when the normal routing constraints say it must stop, for example: no eligible candidate remains, the attempt budget is exhausted, a terminal policy decision is reached, or replay-safety says a retry/failover is unsafe. The timeout duration itself (`30s` in the observed case) is not part of this change unless the later source audit shows a separate defect.

Important invariants for the implementation audit:

- do not special-case this by bypassing `RoutingDecision` / existing failure classification;
- do not create an unlimited retry loop;
- preserve replay-safety handling for requests that may already have reached the upstream;
- preserve credential/provider/model failure-scope semantics and cooldown accounting;
- avoid turning one first-token timeout into an unconditional terminal `channel failed` result when another route can still be tried;
- add regression coverage proving that first-token timeout advances to the next candidate and only becomes terminal after the ordinary exhaustion/terminal conditions are met.

**Execution order:** merge PR #16 first. Then deep-read the then-current first-token-timeout emission, error-classification, retry/failover, replay-safety, failure-scope, and attempt-budget paths and write the detailed hotfix plan before changing code. Complete this hotfix before beginning the P0.2 source audit.

### P0.2 — Global model filter — QUEUED, NOT YET SOURCE-AUDITED

Remembered scope only: add a system-level model-discovery filter that composes with channel-level filtering (intended semantics: both must pass). Exact configuration ownership, regex engine, managed-channel behavior, cache invalidation, API shape, and tests are intentionally undecided until P0.1 and the queued first-token-timeout hotfix are complete.

Upstream reference: Octopus commit `d5a893ff124ca1cb56f2eb25e14380347d247ae9`.

**Do not implement or write a detailed plan from this paragraph.** Re-read the then-current ZyRealm model discovery/sync code first.

### P0.3 — Live request state + manual interrupt — QUEUED, NOT YET SOURCE-AUDITED

Remembered scope only: expose current in-flight routing state and allow a scoped manual interruption of the active attempt/round. ZyRealm should surface its own richer runtime concepts (provider/key/protocol, `RoutingDecision`, `DispatchState`, replay-safety, failure scope) rather than copy Octopus's state object verbatim.

Exact persistence model, SSE/WS transport, retention, interrupt semantics, authorization, UI placement, and interaction with replay safety are intentionally undecided until P0.2 is complete.

**Do not implement or write a detailed plan from this paragraph.** Re-read the then-current relay/log/runtime-state code first.

## P1 — DEFERRED UNTIL ALL P0 GATES PASS

### Credential × model × protocol capability model

Remembered idea only: Octopus's `ChannelKey` / `ChannelModel` / `ChannelGrant` split is a useful authorization-modeling reference. ZyRealm already has a more advanced runtime credential pool, so any future P1 work must layer capability metadata onto the existing scheduler rather than replace it.

No schema, migration, API, or runtime design is approved here. P1 must be source-audited and planned only after P0 is complete and stable.

## Explicit non-goals

- No wholesale merge/rebase from Octopus for these features.
- No P1 database migration while P0 is in progress.
- No replacement of ZyRealm credential fairness, cooldown, circuit breaking, failure-domain classification, protocol fallback, or replay-safety logic with Octopus's simpler routing model.
- No speculative planning of later slices based on today's file layout; each slice must be planned against the then-current `main`.

## Progress checklist

- [x] Gap inventory captured.
- [x] P0.1 source audit completed enough to write an implementation plan.
- [x] P0.1 detailed plan recorded.
- [x] P0.1 implementation completed with full regression evidence on the feature branch.
- [ ] P0.1 merged/stabilized on `main`.
- [x] First-token-timeout failover hotfix requirement captured.
- [ ] First-token-timeout failover source audit + detailed hotfix plan.
- [ ] First-token-timeout failover implemented and merged with regression evidence.
- [ ] P0.2 source audit + detailed plan.
- [ ] P0.2 implemented and merged with full regression evidence.
- [ ] P0.3 source audit + detailed plan.
- [ ] P0.3 implemented and merged with full regression evidence.
- [ ] Re-evaluate whether P1 is still necessary after P0 operational feedback.
