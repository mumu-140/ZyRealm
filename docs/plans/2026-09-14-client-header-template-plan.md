# P0.1 Client-Header Template Implementation Plan

> Scope: P0.1 only. This plan intentionally does not design the global model filter or live request state.
>
> Source baseline: ZyRealm `main@cbe4638b01aa5beb1a46f73dfb41cabaecaf890c`.
>
> Upstream idea reference: Octopus `1c48ee5105042b8eebaba05c05b2773b04e6c7f3`.

## Goal

Add request-scoped template expansion to channel custom-header values so operators can forward selected client metadata to upstream providers without weakening ZyRealm's existing credential and proxy-header isolation.

Example:

```text
X-Upstream-Project: {client_header:OpenAI-Project}
X-Upstream-Tenant: tenant-{client_header:X-Tenant-ID}
```

The implementation must behave consistently for:

- normal HTTP transformed requests;
- HTTP passthrough requests;
- downstream WebSocket `/v1/responses` requests that use upstream WebSocket;
- retries/failover attempts, using the original client metadata rather than mutating shared channel configuration.

## Why this is not a direct upstream cherry-pick

ZyRealm's relay is materially different from Octopus:

- HTTP request headers pass through `relayAttempt.copyHeaders()`.
- Adaptive header isolation captures adapter-owned credentials before client/channel overlays and restores them afterward.
- WebSocket upstream dials build their own headers in `buildUpstreamWSHeaders()` and use those headers in the WS pool key.
- Downstream WebSocket relay requests currently detach from `gin.Context`, so `clientRequestHeaders()` returns no original handshake headers once `relayRequest.c == nil`.

A correct implementation therefore needs a shared renderer plus a request-level source-header snapshot that both HTTP and WS paths can use.

## Current source map

### HTTP

`internal/relay/relay_request.go`

Current order in `relayAttempt.copyHeaders()`:

1. capture adapter credentials when adaptive header isolation is enabled;
2. copy allowed client headers, excluding `hopByHopHeaders` and any `allowClientHeader()` rejection;
3. preserve/merge special `anthropic-beta` behavior;
4. ensure User-Agent exists;
5. apply `ra.effectiveHeaders()` channel custom headers;
6. restore adapter credentials when isolation is enabled.

Both HTTP forwarding paths call this same function:

- `forwardViaHTTPPassthrough()` in `internal/relay/relay_http.go`;
- `forwardViaHTTPStandard()` in `internal/relay/relay_http.go`.

This is the correct common insertion point for HTTP behavior.

### WebSocket

`internal/relay/ws_pool.go`

`buildUpstreamWSHeaders()` independently:

1. copies client headers allowed by `shouldProxyUpstreamWSHeader()`;
2. applies `channel.CustomHeader` literally;
3. overwrites `Authorization` with the selected upstream key;
4. forces `OpenAI-Beta: responses_websockets=2026-02-06`.

The final header set participates in `wsHeaderSignature()` and therefore in `wsPoolKey`. This is correct for tenant/project-sensitive custom headers: different rendered headers must not share the same pooled upstream connection identity.

### Downstream WebSocket request construction

`internal/relay/ws_client.go`

`HandleWSResponse()` accepts the downstream WebSocket from a `gin.Context`, but `newWSRelayRequest()` currently constructs:

```go
&relayRequest{
    c:   nil,
    ctx: ctx,
    ...
}
```

and `relayAttempt.clientRequestHeaders()` returns nil whenever `ra.c == nil`.

Therefore P0.1 must preserve a request-level clone of the original downstream handshake headers before the relay detaches from `gin.Context`.

### Security boundary

`internal/relay/type.go` already excludes at least:

- `authorization`;
- `x-api-key`;
- `proxy-authorization`;
- hop-by-hop connection headers;
- forwarded client-address headers (`x-forwarded-*`, `x-real-ip`, `forwarded`, Cloudflare/client-IP variants);
- `content-length`, `host`, `accept-encoding`, etc.

P0.1 must not create a template side channel that bypasses this existing intent.

## Design decision

### 1. Add one shared, pure renderer

Create a small relay-local helper, suggested file:

`internal/relay/client_header_template.go`

Suggested responsibilities:

```go
func renderClientHeaderTemplate(value string, source http.Header) string
func isAllowedClientHeaderTemplateSource(name string) bool
```

Implementation characteristics:

- recognize `{client_header:<header-name>}` placeholders inside arbitrary surrounding text;
- header lookup must be case-insensitive, using canonical `http.Header` behavior;
- trim placeholder header names;
- unknown/missing headers render to an empty string;
- multiple placeholders in one value are supported;
- plain values without placeholders are returned byte-for-byte unchanged;
- no recursive/template-within-template evaluation;
- no environment-variable, body-field, query-param, cookie, or Go-template functionality in P0.1.

Keep this helper free of channel/database state so it is deterministic and unit-testable.

### 2. Template source policy must be stricter than ordinary forwarding

Do **not** simply allow any header that happens to exist on the client request.

`isAllowedClientHeaderTemplateSource()` should reject at minimum:

- every name present in `hopByHopHeaders`;
- `cookie` and `set-cookie`;
- any future credential-like aliases already recognized by adapter/header isolation code, if that list is broader than `hopByHopHeaders`;
- malformed/empty header names.

Reason: ordinary forwarding rules and template exfiltration have different risk. A user intentionally configuring `X-Leak: {client_header:Authorization}` must not be able to copy a downstream credential into an arbitrary upstream header.

Expected allowed examples:

- `OpenAI-Project`;
- `OpenAI-Organization`;
- `X-Tenant-ID`;
- other non-sensitive application metadata that is not blocked by the relay's header policy.

Do not hardcode an allowlist limited to OpenAI headers; ZyRealm supports arbitrary providers and enterprise metadata.

### 3. Preserve original client headers on `relayRequest`

Add a request-level field, suggested shape:

```go
clientHeaders http.Header
```

Populate it once from the original downstream request with `Clone()`.

HTTP construction path:

- set `clientHeaders` from `c.Request.Header.Clone()` when creating the `relayRequest`;
- retain current `c` behavior for response writing and other request state.

WebSocket construction path:

- capture the initial HTTP upgrade request headers in `HandleWSResponse()` before entering the message loop;
- thread that immutable snapshot through `processWSResponseCreate()` / `newWSRelayRequest()` for both the first attempt and replay request construction;
- do not rebuild the template source from response.create JSON payloads.

Rationale: custom header templates refer to client **HTTP/WS handshake metadata**, not model request body fields.

### 4. Centralize access through a request method

Change `clientRequestHeaders()` to prefer the stored snapshot:

```go
func (r *relayRequest) clientRequestHeaders() http.Header
```

Semantics:

1. if `clientHeaders` exists, return it;
2. otherwise, for backward compatibility in tests/internal paths, fall back to `r.c.Request.Header` when available;
3. otherwise return nil.

Callers should treat the returned header as read-only.

This avoids divergent logic between HTTP and WS.

### 5. Render channel headers per attempt, never mutate `channel.CustomHeader`

Add a helper that renders the effective channel headers against the request snapshot, for example:

```go
func (ra *relayAttempt) renderedEffectiveHeaders() map[string]string
```

or a lower-level function taking the existing map and `http.Header`.

Rules:

- render after channel/attempt overlay resolution (`effectiveHeaders()`), so adaptive/base URL/key attempt semantics stay intact;
- do not write rendered values back into cached `dbmodel.Channel` objects;
- each retry/failover attempt renders from the same immutable client-header snapshot;
- preserve existing header key precedence and adaptive credential restoration order.

### 6. HTTP integration

Modify only the custom-header application portion of `relayAttempt.copyHeaders()`:

Current conceptual line:

```go
for key, value := range ra.effectiveHeaders() {
    outboundRequest.Header.Set(key, value)
}
```

becomes per-request rendered values.

Required ordering remains:

1. outbound adapter defaults/credentials already exist;
2. capture adapter credentials if adaptive isolation is enabled;
3. copy allowed ordinary client headers;
4. apply rendered channel custom headers;
5. restore protected adapter credentials.

This ensures a custom header template cannot replace protected upstream adapter credentials when adaptive isolation is active.

Do not add separate logic in `forwardViaHTTPPassthrough()` or `forwardViaHTTPStandard()`; both already converge on `copyHeaders()`.

### 7. WebSocket integration

Refactor `buildUpstreamWSHeaders()` so custom header values use the same shared renderer and source policy as HTTP.

Preferred signature remains explicit/pure, e.g.:

```go
func buildUpstreamWSHeaders(clientHeaders http.Header, channel *dbmodel.Channel, key string) http.Header
```

but the custom-header loop should render each `HeaderValue` against `clientHeaders`.

Keep final precedence unchanged:

1. safe client headers;
2. rendered channel custom headers;
3. forced upstream `Authorization`;
4. forced Responses WS beta header.

This guarantees templates cannot override the selected upstream key or required WS beta mode.

Because rendered headers remain part of `wsHeaderSignature()`, connection pooling automatically separates different tenant/project header values. Add an explicit test for this invariant.

### 8. Configuration validation at persistence boundary

Do not rely only on frontend validation.

Add channel custom-header template validation in the shared channel save/update path so it covers:

- channel create;
- channel update;
- batch custom-header update;
- any internal/import path that reuses the same persistence helpers, where practical.

Source audit shows channel writes are handled across `internal/server/handlers/channel.go`, `internal/server/handlers/channel_batch.go`, and `internal/op/channel.go`; implementation should place reusable validation low enough that batch operations cannot bypass it.

Validation should reject:

- malformed placeholders such as an unclosed `{client_header:...` expression;
- empty placeholder names;
- blocked sensitive source names (`Authorization`, `X-Api-Key`, `Cookie`, proxy-auth, forwarded IP headers, etc.).

Validation should allow:

- ordinary literal custom-header values;
- mixed literal + allowed placeholders;
- multiple allowed placeholders in one value.

Do not reject a placeholder merely because the header is absent at save time; the client-specific value only exists at request time.

### 9. Frontend affordance only, not a redesign

Use the existing custom-header editor in:

`web/src/components/modules/channel/Form.tsx`

Add compact explanatory text near the value input, with a safe example such as:

```text
{client_header:OpenAI-Project}
```

and state that sensitive authentication/cookie/proxy headers cannot be referenced.

Add translations to all current locale files:

- `web/public/locale/zh_hans.json`;
- `web/public/locale/zh_hant.json`;
- `web/public/locale/en.json`.

No new modal, DSL editor, syntax highlighter, or template preview is needed in P0.1.

## Test-first implementation sequence

### Task 1 — Pure parser/renderer security tests

Create:

`internal/relay/client_header_template_test.go`

Write failing tests first for:

- literal value unchanged;
- single placeholder replacement;
- case-insensitive lookup;
- mixed literal + placeholder;
- multiple placeholders;
- missing allowed header -> empty string;
- blocked Authorization;
- blocked X-Api-Key;
- blocked Cookie;
- blocked proxy/forwarded client identity headers;
- malformed/empty placeholders rejected by validator;
- renderer never recursively expands values read from the client header.

Then implement the smallest shared helper needed to pass.

### Task 2 — HTTP integration tests

Add focused tests around `copyHeaders()` using a request with:

- an allowed source header (`OpenAI-Project` or `X-Tenant-ID`);
- a channel custom header containing the placeholder;
- an adapter credential already present on the outbound request;
- adaptive header isolation enabled where the existing test helpers permit it.

Assert:

- rendered custom header reaches upstream request;
- ordinary safe header copying remains unchanged;
- protected adapter credential remains the adapter credential, not a client/template value;
- HTTP standard and passthrough both rely on the same `copyHeaders()` behavior rather than duplicating template code.

If full forward-path tests are already expensive, unit-test `copyHeaders()` directly and add one end-to-end HTTP relay regression for confidence.

### Task 3 — Preserve downstream WS handshake headers

Add the request-level `clientHeaders` snapshot and thread it through WS request construction.

Tests should prove:

- `newWSRelayRequest()` retains the provided original headers even with `c == nil`;
- replay request reconstruction retains the same source-header snapshot;
- nil/no-header internal call paths remain safe.

### Task 4 — Upstream WS rendering + pool isolation

Extend existing WS pool/header tests or add a focused file.

Assert:

- allowed template renders into the upstream WS handshake;
- selected upstream `Authorization: Bearer <key>` wins over any channel/template attempt;
- required `OpenAI-Beta` wins over channel/template attempts;
- blocked client header sources are not exposed;
- two otherwise-identical requests with different rendered tenant/project values produce different `wsHeaderSignature()` / pool keys;
- identical rendered headers produce the same pool signature.

### Task 5 — Persistence validation

Add unit tests at the reusable validation layer covering both valid and invalid custom-header configs.

Add handler/op regression coverage sufficient to prove batch updates cannot bypass validation.

Do not duplicate parser logic between handler, batch handler, and op layer.

### Task 6 — Frontend hint and locale coverage

Update the existing form only.

Add/extend the lightweight web tests if there is an established pure-logic test location for channel form helpers; otherwise rely on type/lint/build for the static hint and keep parser enforcement backend-side.

Required verification:

```bash
cd web
pnpm lint
pnpm test
pnpm build
```

### Task 7 — Full repository verification

Run the same gates as `.github/workflows/ci.yml`:

```bash
bash scripts/check-governance.sh --repo
mkdir -p static/out
touch static/out/.keep
go vet ./...      # evidence only; current workflow allows the known baseline warning
go test -buildvcs=false ./...
cd web
pnpm install --frozen-lockfile
pnpm lint
pnpm test
pnpm build
```

Do not claim P0.1 complete until the required CI jobs are green on the feature branch/PR.

## Expected file set

Likely backend changes:

- `internal/relay/client_header_template.go` (new)
- `internal/relay/client_header_template_test.go` (new)
- `internal/relay/type.go`
- `internal/relay/relay_request.go`
- `internal/relay/ws_client.go`
- `internal/relay/ws_pool.go`
- related existing WS/relay test files as appropriate
- reusable channel validation in `internal/op/...` or another shared validation file chosen after implementation-time inspection
- channel/batch handler tests as required

Likely frontend changes:

- `web/src/components/modules/channel/Form.tsx`
- `web/public/locale/zh_hans.json`
- `web/public/locale/zh_hant.json`
- `web/public/locale/en.json`

Not expected:

- database migrations;
- changes to routing decision/failure-domain logic;
- changes to balancer/credential fairness;
- protocol transformer changes;
- new dependencies;
- deployment scripts.

## Acceptance criteria

P0.1 is complete only when all of the following are true:

1. A channel custom-header value can interpolate one or more safe client headers.
2. Existing literal custom headers behave exactly as before.
3. HTTP transformed and HTTP passthrough forwarding behave consistently.
4. Downstream WS ingress preserves handshake metadata and upstream WS templates render consistently with HTTP.
5. Sensitive downstream credentials/cookies/proxy-auth/forwarded identity headers cannot be referenced through the template syntax.
6. Upstream adapter credentials remain protected by current isolation/precedence rules.
7. WS connection pool identity includes the final rendered header set, preventing cross-tenant header reuse.
8. Invalid or unsafe template configs are rejected by backend validation, including batch updates.
9. No channel object/cache is mutated per request.
10. No DB migration or new dependency is introduced.
11. Governance, backend tests, frontend lint/test/build are green; `go vet` is checked against the known baseline.

## Stop condition

After P0.1 is implemented and verified, stop. Update the roadmap checkbox/evidence, merge or otherwise stabilize P0.1, and only then begin source audit for P0.2 (global model filter).
