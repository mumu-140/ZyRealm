# P0.1 Client-Header Template Implementation Plan

> Scope: P0.1 only. This plan intentionally does not design the global model filter or live request state.
>
> Source baseline: ZyRealm `main@cbe4638b01aa5beb1a46f73dfb41cabaecaf890c`.
>
> Upstream idea reference: Octopus `1c48ee5105042b8eebaba05c05b2773b04e6c7f3`.
>
> Safety review: revised on 2026-09-14 after tracing the complete HTTP + downstream-WS + upstream-WS data flow. The original idea of reusing a full handshake-header snapshot through `clientRequestHeaders()` is explicitly rejected below because it would broaden existing WS header-forwarding semantics.

## Goal

Add request-scoped template expansion to channel custom-header values so operators can forward selected client metadata to upstream providers without weakening ZyRealm's existing credential, body, protocol, or proxy-header isolation.

Example:

```text
X-Upstream-Project: {client_header:OpenAI-Project}
X-Upstream-Tenant: tenant-{client_header:X-Tenant-ID}
```

The implementation must behave consistently for:

- normal HTTP transformed requests;
- HTTP passthrough requests;
- downstream WebSocket `/v1/responses` requests using upstream WebSocket transform mode;
- downstream WebSocket `/v1/responses` requests using upstream WebSocket passthrough mode;
- WebSocket warmup (`generate:false`);
- WS reconnect/replay paths;
- ordinary retries/provider failover, always using the same immutable request-scoped template source.

## Non-negotiable invariants

### 1. Client-header templates are Header-only metadata

A template source must never be inserted into or serialized with:

- `rawBody`;
- `InternalLLMRequest`;
- `reqBody map[string]json.RawMessage`;
- transformed OpenAI/Anthropic/Responses JSON;
- WS `response.create` payloads;
- replay conversation state;
- parameter override JSON.

The template source is a separate request-scoped metadata field used only while constructing outbound HTTP or WS **headers**.

This is the primary structural guarantee that `{}`, quotes, commas, backslashes, JSON-looking strings, or other legal header characters cannot corrupt request JSON.

### 2. Do not broaden existing ordinary header forwarding

Current behavior matters:

- HTTP ingress has a live `gin.Context`, and ordinary safe client headers are copied by `copyHeaders()`.
- downstream WS requests create `relayRequest{c:nil,...}`;
- therefore `clientRequestHeaders()` currently returns `nil` for downstream WS ingress;
- upstream WS therefore does **not** currently receive a general copy of downstream WS handshake headers.

P0.1 must preserve that behavior.

**Rejected design:** storing all downstream handshake headers in `relayRequest.clientHeaders` and making `clientRequestHeaders()` return that snapshot.

Why rejected: that would cause downstream WS to begin forwarding ordinary handshake metadata that it did not forward before. Some upstreams may reject unexpected Origin, subprotocol, SDK, tenant, tracing, or provider-specific headers. That is a compatibility change unrelated to templates.

### 3. Template problems must not corrupt the body or masquerade as upstream transport failures

A malformed, missing, denied, or invalid template result must be handled before network dispatch and must never:

- mutate request JSON;
- cause a JSON parser error;
- be classified as a provider/credential outage;
- poison circuit-breaker/cooldown state;
- trigger credential rotation merely because local template rendering failed.

Save/update validation should reject invalid configuration early. Runtime rendering remains defensive for imported/legacy/stale configuration.

### 4. Sensitive client data must not enter the template snapshot

Authorization/cookie/proxy-auth/client-IP/hop-by-hop/WS-handshake-control headers are excluded **when the snapshot is created**, not merely when a placeholder is later evaluated.

This gives defense in depth: secrets are not retained in the template source object at all.

## Current source trace

### HTTP body and relay construction

`internal/relay/relay_parse.go`

Current order:

1. `io.ReadAll(c.Request.Body)` reads original body bytes.
2. inbound adapter `TransformRequest(..., body)` parses/transforms the body.
3. `InternalLLMRequest.Validate()` validates it.
4. `newRelayHandler()` resolves group/routing state.
5. only then does `buildRelayHandler()` construct `relayRequest`.

Therefore a template-source snapshot added to `relayRequest` can remain completely outside body parsing.

### HTTP outbound header application

`internal/relay/relay_request.go`

Current `copyHeaders()` order:

1. capture adapter credentials when adaptive header isolation is enabled;
2. copy allowed ordinary client headers, excluding `hopByHopHeaders` and `allowClientHeader()` rejects;
3. preserve/merge `anthropic-beta`;
4. ensure User-Agent exists;
5. apply `ra.effectiveHeaders()` channel custom headers;
6. restore adapter credentials.

Both HTTP forwarding paths use it:

- `forwardViaHTTPPassthrough()`;
- `forwardViaHTTPStandard()`.

Template rendering belongs only in step 5.

### Downstream WS JSON path

`internal/relay/ws_client.go`

Current `response.create` flow is structurally separate from request headers:

1. `websocket.Accept(...)` upgrades the connection.
2. `conn.Read(ctx)` receives a WS text frame as `data []byte`.
3. `json.Unmarshal(data, &msg)` validates event type.
4. `processWSResponseCreate()` unmarshals `data` into `map[string]json.RawMessage`.
5. it removes/adds WS-only fields such as `type`, `generate`, `previous_response_id`, `stream`.
6. it marshals that map to `bodyBytes`.
7. the inbound adapter parses `bodyBytes` into `InternalLLMRequest`.
8. `newWSRelayRequest()` constructs `relayRequest{c:nil,...}`.

The header-template source must be passed beside `data/bodyBytes`; it must never be merged into those values.

### Upstream WS body path

Two independent WS payload builders exist:

- transform mode in `internal/relay/relay_websocket.go` marshals a Responses request to JSON and sends it with `SendResponseCreate()`;
- passthrough mode in `internal/relay/ws_passthrough.go` parses/mutates/re-marshals its JSON payload and sends it with `SendRaw()`.

Neither path needs template values in the payload. P0.1 must not modify these JSON-building functions except tests proving they remain unaffected.

### Upstream WS headers and pool identity

`internal/relay/ws_pool.go`

`buildUpstreamWSHeaders()` currently:

1. copies ordinary client headers accepted by `shouldProxyUpstreamWSHeader()`;
2. applies `channel.CustomHeader` literally;
3. forces upstream `Authorization`;
4. forces Responses WS `OpenAI-Beta`.

The final header set participates in `wsHeaderSignature()` / `wsPoolKey`.

This is desirable for template-rendered tenant/project metadata: two different rendered custom-header sets must not reuse the same upstream pooled connection.

## Corrected design

### 1. Add a dedicated sanitized template-source snapshot

Add a request-level field with a narrow purpose, for example:

```go
templateHeaderSource http.Header
```

or a small immutable relay-local wrapper.

Do **not** call it `clientHeaders`; the field is not a general header-forwarding source.

Create a helper such as:

```go
func snapshotClientHeaderTemplateSource(src http.Header) http.Header
```

Properties:

- create a new map/slices; never retain the caller's mutable map;
- canonical/case-insensitive header behavior;
- include only source names accepted by `isAllowedClientHeaderTemplateSource()`;
- never copy blocked credential/cookie/proxy/forwarded/WS-control headers;
- callers treat the returned snapshot as immutable;
- do not log values.

This source snapshot is request metadata only and is not serialized.

### 2. Preserve `clientRequestHeaders()` exactly as an ordinary-forwarding accessor

Do **not** change its semantics for P0.1.

Conceptually it remains:

```go
func (ra *relayAttempt) clientRequestHeaders() http.Header {
    if ra == nil || ra.c == nil || ra.c.Request == nil {
        return nil
    }
    return ra.c.Request.Header
}
```

Consequences:

- HTTP ordinary-header forwarding remains unchanged.
- downstream WS (`c == nil`) still does not suddenly forward general handshake headers upstream.
- templates use `templateHeaderSource`, not `clientRequestHeaders()`.

This separation is a required compatibility boundary.

### 3. Capture template metadata without touching JSON

#### HTTP ingress

In `buildRelayHandler()`, after request parsing/validation has already succeeded, create:

```go
templateHeaderSource: snapshotClientHeaderTemplateSource(c.Request.Header)
```

This cannot alter the already-read `rawBody` or `InternalLLMRequest`.

#### Downstream WS ingress

In `HandleWSResponse()`, snapshot template-eligible handshake metadata once from the original upgrade request, preferably immediately before `websocket.Accept()`:

```go
templateHeaderSource := snapshotClientHeaderTemplateSource(c.Request.Header)
```

Then thread that separate value through:

- `processWSResponseCreate(...)`;
- `newWSRelayRequest(...)`;
- replay reconstruction;
- warmup path.

Do not place it in `reqBody`, `bodyBytes`, `InternalLLMRequest`, conversation state, or raw replay JSON.

### 4. Shared parser/renderer must return structured outcome

Create a relay-local helper, suggested file:

`internal/relay/client_header_template.go`

Suggested conceptual API:

```go
type clientHeaderTemplateResult struct {
    Value  string
    Apply  bool
    Reason string // diagnostic category only, never a secret value
}

func renderClientHeaderTemplate(value string, source http.Header) clientHeaderTemplateResult
func validateClientHeaderTemplate(value string) error
func isAllowedClientHeaderTemplateSource(name string) bool
```

The exact type may differ, but rendering must distinguish:

- literal/no-template value -> apply unchanged;
- all referenced sources present and valid -> apply rendered value;
- missing source -> do not synthesize malformed partial metadata;
- denied/malformed source -> do not apply that custom header;
- invalid rendered HTTP field value -> do not apply that custom header.

Do not make request-body functions aware of this result.

### 5. Missing/invalid dynamic metadata is non-blocking at runtime

Upstream Octopus replaces a missing source with an empty string. ZyRealm should be more defensive for availability.

P0.1 runtime policy:

- configuration-time malformed/denied templates are rejected on save/update/import paths where validation is available;
- if runtime source metadata is absent, skip **that configured custom header** rather than abort the whole LLM request;
- if a rendered value is invalid as an HTTP header field value, skip that custom header and emit a value-free diagnostic;
- do not return a provider/network failure for local template resolution;
- do not mutate circuit/cooldown/credential health because of a local template problem.

This avoids a missing optional `OpenAI-Project`/tenant metadata header turning into a local JSON/transport failure.

If the upstream provider truly requires that metadata, it may still reject the request semantically; that is unavoidable when the client did not supply required metadata. The relay should nevertheless remain structurally correct and observable.

### 6. Header syntax validation

ZyRealm already depends directly on `golang.org/x/net`, so implementation may use existing HTTP header validation utilities such as `httpguts.ValidHeaderFieldName` / `ValidHeaderFieldValue` if appropriate, without adding a dependency.

At minimum validate:

- custom header key syntax;
- placeholder source-name syntax;
- final rendered header value;
- no CR/LF/NUL/control-character injection.

Do not JSON-escape header values. Headers and JSON are distinct protocol fields; JSON escaping a Header would be incorrect.

### 7. Template source policy

`isAllowedClientHeaderTemplateSource()` should reject at minimum:

- every name in `hopByHopHeaders`;
- `authorization`;
- `x-api-key`;
- `cookie` / `set-cookie`;
- `proxy-authorization` / proxy authentication variants;
- forwarded/client address identity headers;
- `host` / `content-length` / `accept-encoding`;
- `connection` / `upgrade`;
- every `sec-websocket-*` header;
- malformed/empty names;
- any additional credential aliases recognized by adaptive header-isolation code.

Expected allowed examples include:

- `OpenAI-Project`;
- `OpenAI-Organization`;
- `X-Tenant-ID`;
- other non-sensitive application metadata.

Do not hardcode an OpenAI-only allowlist; ZyRealm supports arbitrary providers.

### 8. Per-attempt rendering; never mutate channel/cache objects

Render channel custom-header values per relay attempt from:

- the current attempt's effective channel header configuration; and
- the immutable request-level `templateHeaderSource`.

Do not write results into:

- `dbmodel.Channel.CustomHeader`;
- cached channel objects;
- group state;
- replay state.

Retries/failover must reuse the same source snapshot but render against each attempt's own channel header config.

### 9. HTTP integration is header-only

Modify only the custom-header loop inside `copyHeaders()`.

Required order remains:

1. adapter creates outbound request/body;
2. adapter credentials exist;
3. adaptive isolation captures credentials when enabled;
4. ordinary safe client headers are copied exactly as today;
5. channel custom headers are rendered against `templateHeaderSource`;
6. only successful render results are set;
7. protected adapter credentials are restored.

No changes are required to:

- `TransformRequest()` body parsing;
- `TransformRequestRaw()` raw body;
- parameter overrides;
- stream payload serialization.

### 10. WS integration requires two logically separate header inputs

Do not overload one `http.Header` parameter with two meanings.

The WS dial/header builder should conceptually receive:

1. `forwardHeaders` — existing ordinary client headers, preserving current semantics;
2. `templateHeaderSource` — sanitized metadata used only to render configured channel custom headers.

For example, signatures may evolve toward:

```go
func buildUpstreamWSHeaders(
    forwardHeaders http.Header,
    templateHeaderSource http.Header,
    channel *dbmodel.Channel,
    key string,
) http.Header
```

or an equivalent small context struct.

For downstream WS ingress:

- `forwardHeaders` remains `nil` as it is today;
- `templateHeaderSource` contains only safe snapshotted handshake metadata.

For HTTP ingress that later uses upstream WS:

- ordinary forwarding behavior continues to use current `clientRequestHeaders()`;
- template rendering uses the sanitized snapshot.

Final WS precedence remains:

1. existing ordinary forwarded headers;
2. rendered channel custom headers;
3. forced selected upstream `Authorization`;
4. forced Responses WS `OpenAI-Beta`.

### 11. Preserve WS pool isolation

`wsHeaderSignature()` must continue to hash/sign the **final outbound header set**.

Required invariant:

- same channel/key + same rendered custom headers -> reusable pool identity;
- same channel/key + different rendered tenant/project header -> different pool identity.

Do not put raw template source headers into the pool key if they are not actually emitted upstream. The pool identity should reflect final outbound behavior, not unused client metadata.

### 12. Warmup must use the same template metadata

Current downstream WS `generate:false` warmup eventually calls `TryUpstreamWS(..., nil)` without request header metadata.

That becomes insufficient when a channel's upstream WS handshake requires a rendered client-header template.

Thread `templateHeaderSource` into:

- `bestEffortWarmupUpstreamWS()`;
- `warmupUpstreamWSConnection()`;
- the WS header builder.

But preserve `forwardHeaders == nil` for downstream WS ordinary forwarding.

This ensures warmup targets the same rendered-header pool identity as the real request rather than warming an unusable generic connection.

### 13. Replay/reconnect must retain only the sanitized template source

Both transform and passthrough WS reconnect paths call `TryUpstreamWS*()` again.

They must reuse the same request-level `templateHeaderSource` so a reconnect cannot silently lose tenant/project headers or switch pool identity.

Replay must not copy template metadata into replay JSON or conversation state. It remains request-scoped metadata attached to the newly constructed `relayRequest` for that downstream connection/request.

## Configuration validation

Backend validation must cover channel custom headers across:

- create;
- update;
- batch custom-header update;
- practical import/restore paths that reuse shared validation.

Validation should reject:

- malformed/unclosed `{client_header:...` syntax;
- empty source names;
- denied sensitive source names;
- invalid source header-name syntax;
- invalid custom output header-name syntax;
- literal values containing invalid HTTP header control characters.

Validation should allow:

- literal custom-header values;
- mixed literal + allowed placeholders;
- multiple placeholders;
- absent runtime source values (not knowable at save time);
- legal JSON-looking text inside header values, because it remains a header value.

Do not duplicate parser logic in create/update/batch handlers.

## Frontend scope

Use the existing custom-header editor in:

`web/src/components/modules/channel/Form.tsx`

Add a compact syntax hint such as:

```text
{client_header:OpenAI-Project}
```

State that authentication/cookie/proxy/forwarding/WS-control headers cannot be referenced.

Update:

- `web/public/locale/zh_hans.json`;
- `web/public/locale/zh_hant.json`;
- `web/public/locale/en.json`.

No new editor/modal/DSL UI is needed.

## Test-first implementation sequence

### Task 1 — Parser, source filtering, and renderer tests

Create:

`internal/relay/client_header_template_test.go`

Write failing tests first for:

- literal value unchanged;
- single placeholder replacement;
- case-insensitive lookup;
- mixed literal + placeholder;
- multiple placeholders;
- missing source -> target custom header is skipped, request is not failed;
- blocked Authorization;
- blocked X-Api-Key;
- blocked Cookie;
- blocked proxy/forwarded client identity headers;
- blocked `Sec-WebSocket-*` source;
- malformed/empty placeholders rejected by validator;
- source header values are not recursively interpreted as templates;
- JSON-looking legal header text remains byte-for-byte a header value;
- CR/LF/control-character result is rejected/skipped;
- template source snapshot never contains blocked credentials/cookies/WS-control headers.

### Task 2 — Body/JSON isolation regression tests

These tests are mandatory because P0.1 must prove absence of cross-layer mutation.

HTTP transformed path:

- construct a valid JSON request body;
- record the body/internal request before header application;
- render/apply template headers;
- assert body bytes/semantic payload are unchanged.

HTTP passthrough path:

- record `rawBody` before `copyHeaders()`;
- assert it remains identical after header application.

WS transform path:

- use a `response.create` payload containing nested objects/arrays/tool fields;
- provide template-source values containing quotes/braces/commas/backslashes;
- assert the generated WS JSON is valid and semantically identical to baseline except pre-existing WS transformations (`type`/`stream` etc.);
- assert template values appear only in outbound headers.

WS passthrough path:

- perform the same separation check around `buildWSPassthroughRequestPayload()`;
- assert no template metadata is inserted into payload JSON.

### Task 3 — HTTP integration and compatibility

Tests around `copyHeaders()` must prove:

- rendered custom header reaches outbound request;
- ordinary safe header copying is unchanged;
- missing dynamic source skips only that configured custom header;
- adapter credentials remain protected under adaptive header isolation;
- no template failure becomes a provider/credential routing failure;
- standard and passthrough paths share the same header logic.

### Task 4 — Downstream WS snapshot isolation

Tests must prove:

- only template-eligible handshake metadata is snapshotted;
- Authorization/Cookie/Upgrade/Connection/Sec-WebSocket values are absent from the snapshot;
- `newWSRelayRequest()` retains the sanitized template source with `c == nil`;
- `clientRequestHeaders()` still returns nil for downstream WS and its existing semantics are unchanged;
- replay reconstruction carries the same sanitized template source separately from JSON/body state.

This task specifically guards against the rejected over-broad design.

### Task 5 — Upstream WS transform + passthrough + reconnect

Tests must prove:

- transform mode renders allowed template values into handshake headers;
- passthrough mode does the same;
- forced upstream `Authorization` still wins;
- required WS `OpenAI-Beta` still wins;
- reconnect/redial preserves the same template-derived headers;
- ordinary downstream WS handshake headers are **not** newly forwarded merely because template support exists.

### Task 6 — WS pool identity and warmup

Tests must prove:

- different final rendered tenant/project headers -> different `wsHeaderSignature()` / pool key;
- identical final rendered headers -> same signature;
- unused template-source metadata does not alter pool identity;
- `generate:false` warmup uses the same template metadata as the subsequent real request;
- warmup does not start forwarding ordinary downstream WS handshake headers.

### Task 7 — Persistence validation

Add reusable validation-layer tests for valid/invalid custom-header templates.

Prove batch updates cannot bypass validation.

Where possible, imported/legacy invalid data should be handled defensively at runtime by skipping the affected custom header rather than crashing or corrupting routing state.

### Task 8 — Frontend hint/locales

Update only the existing form and three locale files.

Required frontend checks:

```bash
cd web
pnpm lint
pnpm test
pnpm build
```

### Task 9 — Full repository verification

Run CI-equivalent gates:

```bash
bash scripts/check-governance.sh --repo
mkdir -p static/out
touch static/out/.keep
go vet ./...      # evidence only; compare with known baseline
go test -buildvcs=false ./...
cd web
pnpm install --frozen-lockfile
pnpm lint
pnpm test
pnpm build
```

Do not claim P0.1 complete until branch/PR CI is green.

## Expected file set

Likely backend changes:

- `internal/relay/client_header_template.go` (new)
- `internal/relay/client_header_template_test.go` (new)
- `internal/relay/type.go`
- `internal/relay/relay_handler.go`
- `internal/relay/relay_request.go`
- `internal/relay/ws_client.go`
- `internal/relay/ws_pool.go`
- `internal/relay/relay_websocket.go`
- `internal/relay/ws_passthrough.go`
- related relay/WS tests
- shared channel custom-header validation under `internal/op/...` or another existing shared validation layer chosen during implementation
- channel/batch validation tests

Likely frontend changes:

- `web/src/components/modules/channel/Form.tsx`
- `web/public/locale/zh_hans.json`
- `web/public/locale/zh_hant.json`
- `web/public/locale/en.json`

Not expected:

- database migrations;
- routing/failure-domain algorithm changes;
- balancer/credential-fairness changes;
- protocol transformer changes;
- request-body schema changes;
- JSON schema changes;
- new dependencies;
- deployment changes.

## Acceptance criteria

P0.1 is complete only when all are true:

1. Safe client metadata can be interpolated into configured custom headers.
2. Literal custom headers behave as before.
3. HTTP transformed and passthrough paths behave consistently.
4. Downstream WS transform and passthrough paths render templates without broadening ordinary handshake-header forwarding.
5. Template metadata remains structurally separate from all JSON/body/replay state.
6. Complex JSON bodies remain semantically unchanged by template rendering.
7. Sensitive downstream credentials/cookies/proxy/forwarded/WS-control headers are absent from the template source and cannot be referenced.
8. Missing/invalid runtime metadata cannot corrupt JSON, cannot create header injection, and does not by itself abort the relay as a provider failure.
9. Upstream adapter credentials and forced WS beta headers retain existing precedence.
10. WS pool identity is based on final emitted headers, so cross-tenant connection reuse cannot occur.
11. WS warmup/reconnect use the same template metadata as the real request.
12. Invalid configs are rejected at backend persistence boundaries; runtime remains defensive for stale/imported data.
13. No per-request mutation of channel/cache objects occurs.
14. No DB migration, request-body schema change, or new dependency is introduced.
15. Governance, backend tests, frontend lint/test/build are green; `go vet` is checked against baseline.

## Stop condition

After P0.1 is implemented and verified, stop. Record evidence in the roadmap, stabilize/merge P0.1, and only then begin source audit for P0.2.
