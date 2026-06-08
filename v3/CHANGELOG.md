# Changelog

All notable changes to `precision-soft/melody/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.7.0] - 2026-06-08 - Platform Extensions: Messaging, Realtime, Auth, i18n, OpenAPI, Lock, Mailer, and Storage

### Added

- `messagebus/` — transport-agnostic asynchronous message bus. Messages are wrapped in `Envelope`s carrying `Stamp`s (`BusNameStamp`, `SentStamp`, `ReceivedStamp`, `HandledStamp`); a configurable middleware stack via `NewManager(name, ...Middleware)` with `NewSendMessageMiddleware(routingByType)` (routes typed messages to a transport) and `NewHandleMessageMiddleware(locator)` (dispatches to handlers — a message with no registered handler is logged as a warning so the loss is observable, and `NewHandleMessageMiddlewareWithOptions(locator, HandleOptions{RequireHandler: true})` turns that into an error so a forgotten registration fails loudly instead of being acked and discarded); typed handler registration through `NewHandlerLocator` plus generic `RegisterHandler[T]`; an in-process `NewInMemoryTransport(bufferSize)`; and a long-running `melody:messagebus:consume` command via `NewConsumeCommand(bus, transports)`. The consumer owns the retry policy: `NewConsumeCommandWithRetry(bus, transports, RetryPolicy{MaxRetries, BaseDelay, FailureTransport})` requeues a failing message with an incremented `RedeliveryStamp` (and an optional backoff `DelayStamp` for delay-aware transports) up to `MaxRetries`, then routes the exhausted message to a dead-letter `FailureTransport` if configured — and if that `FailureTransport.Send` itself fails the message is requeued on the source rather than acked-and-lost — or otherwise nacks it without requeue so a transport-native dead-letter (e.g. the AMQP DLX) can claim it rather than dropping it silently; `NewConsumeCommand` defaults to three retries. The `messagebus/contract.Transport` interface (`Send`/`Receive`/`Ack`/`Nack`/`Close`) lets durable backends plug in (see the `amqp` integration); `Close` releases a transport's consumption resources and is idempotent. Transport lifecycle is owned by the application, not the consume command (see the `### Changed` note below) — `Close` is called by the application during shutdown, not by the consumer.
- `security/` — stateless bearer-token authentication for APIs. `NewBearerTokenSource(validator)` extracts an `Authorization: Bearer <token>` header and delegates to a pluggable `securitycontract.TokenValidator`. Two validators ship: `NewJwtTokenValidator(JwtConfig)` (dependency-free HS256 verification with constant-time signature comparison, a required `exp` claim by default — opt out with `JwtConfig{AllowWithoutExpiry: true}` so a missing `exp` never silently yields a non-expiring token — plus `nbf`/`iat` checks with configurable leeway and optional `iss`/`aud` enforcement, mapping the subject and roles claims to `Claims`; out-of-range or non-finite `NumericDate` claims are rejected as malformed instead of saturating on the int64 conversion) and `NewOpaqueTokenValidator(store)` backed by a `securitycontract.TokenStore` (`NewInMemoryTokenStore` ships for tests/dev) for revocable tokens — the in-memory store adds `DeleteByUser` (sign-out-everywhere) and `PurgeExpired` (janitor cleanup) on top of per-token `Delete`. `NewJsonEntryPoint()` (401, with a `WWW-Authenticate: Bearer` challenge header per RFC 7235) and `NewJsonAccessDeniedHandler()` (403) return JSON instead of redirecting, for pure-API firewalls; `security/config.FirewallOverrideConfiguration` gains `WithEntryPoint`/`WithAccessDeniedHandler` so a single API firewall can answer with JSON while the global web firewall keeps redirecting. The `security` service resolver also exposes `FirewallManagerFromResolver`/`FirewallManagerMustFromResolver` to match the other modules. For applications that resolve roles after the token is validated, `Claims` carries generic `Scope`/`Attributes` maps (the JWT validator can populate `Scope` from a configurable `JwtConfig.ScopeClaim`), and `NewBearerTokenSourceWithEnricher(validator, enricher)` runs a generic `securitycontract.TokenEnricher` after signature validation to turn that scope into the final roles/attributes (e.g. via a database lookup) — tenant- or product-specific resolution lives in the application's enricher, the library stays generic; an enrichment failure falls back to an anonymous token. `Claims` carries explicit `json` tags because a persisted token store (the `integrations/rueidis` Redis store) serializes it as JSON and its Lua index reads the `UserIdentifier` key back — the tags pin that wire contract so a later field rename cannot silently break the per-user token index.
- `translation/` — i18n message catalogs. `NewManager(defaultLocale, fallbackLocales, ...Catalog)` resolves a message id by domain/locale with fallback-chain lookup; `NewMapCatalog(locale)` for in-code messages and `NewJsonDirectoryLoader(directory)` to load `<domain>.<locale>.json` files; an ICU-subset message formatter supporting `{placeholder}`, `plural`, and `select` blocks with per-locale plural rules (recursion depth-guarded against pathological catalog strings); a plural argument that is missing or non-numeric resolves the `other` branch with an empty `#` rather than being treated as the number zero.
- `openapi/` — OpenAPI 3.0.3 generation from route metadata. `NewRegistry()` plus `Describe(routeName, Descriptor)` associates request/response Go types with routes; the generator reflects struct types into JSON Schema (honoring `json` and `validate` tags, with cycle detection) and emits a `Document`; `NewGenerateCommand(info, registry)` writes the spec to stdout or a file. Named struct types are emitted once into `components/schemas` and referenced by `$ref` (deduplicating types reused across operations); pointer fields are marked `nullable`; the fields of an untagged embedded struct are promoted into the parent as encoding/json does; `min`/`max` emit `minLength`/`maxLength` on string fields only — matching what the framework validator actually enforces (`min`/`max` are string-length checks) so the generated spec never advertises a numeric or collection bound the server does not enforce — while `greaterThan`/`lessThan` emit an exclusive `minimum`/`maximum`; and the `Document`/`Schema` types now carry `servers`/`security`/`tags`/`externalDocs` and `description`/`maximum`/`exclusiveMaximum`. `service_resolver.go` registers the registry as a first-class container service (`ServiceOpenApiRegistry`, `RegistryMustFromContainer`/`RegistryMustFromResolver`) so the module wires like the others, and `SpecHandler(info, registry)` serves the document as JSON from a live route (e.g. `GET /openapi.json`) by reusing `Generate` against the running router — every instance behind a load balancer serves the same spec from its identical route table.
- `lock/` — distributed/named lock abstraction. `lock/contract.Locker`/`Lock` (`Acquire`/`Release`/`Refresh`); a dependency-free `NewInMemoryLocker(clock)` with token-owned, TTL-aware, reentrant locks. Durable backends implement the same contract — Redis (`integrations/rueidis/v3`, TTL auto-expiry) and MySQL `GET_LOCK` (`integrations/bunorm/mysql/v3`); note that MySQL advisory locks are connection-lifetime and do not auto-expire, so their `ttl` is documented as not honored as an expiry (the lock releases on `Release` or connection drop).
- `mailer/` — pluggable email sending. `mailer/contract` (`Mailer`, `Transport`, `Message`, `Address`, `Attachment`); `NewManager(transport)` validates sender and recipients; `NewSmtpTransport(SmtpConfig)` (stdlib `net/smtp`) and `NewInMemoryTransport()` for tests. The SMTP transport opportunistically STARTTLSes, and `SmtpConfig` adds `RequireTls` (fail closed unless the connection is encrypted, closing the silent-downgrade hole), `RequireAuth` (fail closed when a username is configured but the server does not advertise the AUTH extension, rather than delivering unauthenticated), `ImplicitTls` (dial straight into TLS, the smtps/465 convention) and a `TlsConfig` override. `RenderMessage` builds RFC 5322 / MIME messages: quoted-printable bodies (kept within the SMTP 998-character line limit), `multipart/alternative` for text+HTML, `multipart/mixed` with base64-encoded attachments, CRLF-stripped headers/addresses, reserved-header filtering so callers cannot override the structural headers, and RFC 2047 encoded-words for non-ASCII subjects and display names (RFC 2231 for non-ASCII attachment filenames) so international text survives transport.
- `storage/` — object-storage abstraction. `storage/contract.Storage` (`Put`/`Get`/`Delete`/`Exists`/`PresignedUrl`); a `NewLocalStorage(baseDirectory)` filesystem backend with base-directory escape protection (textual `..` containment plus symlink-resolution so a symlink planted inside the base cannot point outside it — the symlink walk stops at the base boundary, so the base directory is created lazily on first write instead of the walk ascending into a trusted ancestor and falsely rejecting) that writes objects with restrictive `0o640`/`0o750` permissions. An S3-compatible backend ships in `integrations/awss3/v3`.
- `http/server_sent_event.go`, `http/server_sent_event_hub.go` — Server-Sent Events. `NewServerSentEventWriter(w)` streams `text/event-stream` (sets the headers and flushes after every `Send`); `NewServerSentEventHub()` is a topic-keyed fan-out (`Subscribe`/`Unsubscribe`/`Broadcast`) with **at-most-once** delivery — a full subscriber buffer drops the event, and `DroppedEventCount()` exposes the cumulative loss as a metric. `Shutdown()` drains and closes every subscriber channel so in-flight handler loops exit during a graceful server stop, and `ServerSentEventWriter.Ping()` writes a keepalive comment that a handler can drive from a ticker to hold idle connections open through intermediary proxies. `ServerSentEventSubscriber.DroppedCount()` exposes the per-subscriber dropped-event count so a handler can detect and close a persistently slow subscriber. For load-balanced deployments the hub takes an optional `ServerSentEventBackplane` (`SetBackplane`): `Broadcast` then fans out to local subscribers via `DeliverLocal` **and** replicates to the other instances through the backplane, which calls `DeliverLocal` for events other instances publish — so a client connected to any instance receives every broadcast. A nil backplane keeps the original local-only behaviour; `BackplaneFailures()` counts replications that could not be published. Concrete backplanes ship in `integrations/rueidis` (Redis pub/sub) and `integrations/amqp` (fanout). The same hub backs the WebSocket integration, so both transports fan out across instances.
- `security/` — `AuthenticatedToken` now carries the enriched claims beyond roles: `NewAuthenticatedTokenFromClaims` populates `Scope()` and `Attributes()` (both returning defensive copies) so a `TokenEnricher` can attach tenant/attribute data that attribute-based access control reads downstream — previously only roles survived enrichment and the rest was silently dropped.
- `messagebus/` — the `melody:messagebus:consume` command gains a `--concurrency` flag (N worker goroutines reading the transport concurrently; per-transport Ack/Nack stay serialized) and a bounded graceful-drain window configurable via `ConsumeCommand.WithShutdownGrace(d)`: after an interrupt the consumer waits up to the grace period (default 30s) for in-flight handlers to finish, then stops waiting so a wedged, non-context-aware handler cannot block shutdown indefinitely.
- `mailer/` — `RenderMessage` now emits a unique RFC 5322 `Message-ID` header (random local part + the sender's domain) unless the caller already supplied one in `Headers`.
- `messagebus/` — generic routing builder `NewRouting()` + `RouteType[T](routing, name, transport)` (and `NewSendMessageMiddlewareFromRouting`) so a dispatch bus is wired without hand-writing the `map[reflect.Type]TransportRouting` literal and `reflect.TypeOf` keys; the existing map-based `NewSendMessageMiddleware` is unchanged.
- `http/` — `JsonHandler[Req](handle, ...options)` wraps a handler that needs a JSON body: it decodes the body into `Req`, runs the container validator, and on failure returns an error (or a caller-supplied shape via `WithJsonHandlerErrorResponder`), so per-handler decode/validate boilerplate collapses to the typed `handle` function.
- `openapi/` — `DescribeTyped[Req, Resp](registry, routeName, status, ...options)` describes a route's request/response from Go types via `WithSummary`/`WithDescription`/`WithTags`/`WithResponse[T]`, removing the `Descriptor{RequestType: TypeOf[…](), Responses: map[int]reflect.Type{…}}` literal; the existing `Describe` is unchanged (still used for no-body or multi-response routes).
- `container/` — documented that type registration is on by default, so any registered service is resolvable by its contract type via `MustFromResolverByType[T]` — a single-implementation service needs no string-name constant or per-type accessor (named registration remains for coexisting multi-implementation contracts, which are strict).
- `validation/` — `lessThan` constraint (`NewLessThan(max)`, the `LessThan` type, and the `ConstraintLessThan` registration). A field tagged `validate:"lessThan(value=N)"` (or the shorthand `validate:"lessThan=N"`) now enforces an exclusive upper bound on numeric fields, mirroring the existing `greaterThan` lower-bound constraint and the exclusive `maximum`/`exclusiveMaximum` the `openapi` generator already emits for it.

### Changed

- `security/` — `JwtConfig` gains `RejectFutureIssuedAt` (default `false`). A future `iat` is no longer rejected by default (RFC 7519 treats `iat` as informational, and the previous strict check false-rejected tokens from issuers whose clocks ran ahead of the verifier's leeway); set the flag to restore strict rejection. The contract adds a `RevocableTokenStore` interface (`TokenStore` plus `Put`/`PutWithTtl`/`Delete`/`DeleteByUser`/`PurgeExpired`) so a custom revocable store can be swapped in through the interface rather than only the concrete `InMemoryTokenStore`.
- `messagebus/` — the consumer no longer calls `Close` on the transport it was handed; transport lifecycle is owned by the application (the process exit releases AMQP connections), so a transport shared between the dispatcher and the consumer is no longer disabled when the consume command returns. The in-memory transport now honors a `DelayStamp` on requeue (delayed re-push instead of an immediate hot retry). The retry backoff is capped (`maxRetryDelay`) and overflow-safe.
- `security/` — the `securitycontract.Token` interface gains `Scope() map[string]any` and `Attributes() map[string]any` so the enriched scope/attribute claims a `TokenEnricher` attaches are readable through the contract for attribute-based access control downstream — previously only `*AuthenticatedToken` carried them and a consumer holding a `Token` had to type-assert. `AnonymousToken` returns empty maps and the `Token` wrapper delegates to the wrapped token. (Additive method on an exported interface — external `Token` implementers must add the two methods.)

### Fixed

- `openapi/` — the `validate` tag parser now understands the canonical parenthesized constraint syntax (`min(value=3)`, `max(value=64)`, `regex(pattern=^x$)`, `greaterThan(value=0)`) that the `validation` package enforces and the docs prescribe. The previous parser split each rule on the first `=` only, so a parenthesized constraint produced a garbage rule name and emitted no `minLength`/`maxLength`/`pattern`/`minimum` — silently under-specifying the generated spec relative to the runtime validator. Rule splitting is also group-aware now, so a comma inside `{n,m}` or a character class no longer truncates a `regex`/`pattern` value.
- `openapi/` — a `requestBody` is no longer attached to body-less HTTP methods. `buildOperation` added the descriptor's request schema to every verb of a multi-method route, so a route exposing `GET`/`HEAD`/`DELETE` alongside `POST` emitted a `requestBody` (with `required: true`) on the `GET`/`HEAD`/`DELETE` operations — invalid for `GET`/`HEAD` and breaking generated clients; the body is now emitted only for `POST`/`PUT`/`PATCH`.
- `validation/` — the shorthand `regex=PATTERN` tag is now enforced instead of failing open. `createConstraintWithParams` read the regex pattern only from `params["pattern"]` (set by the parenthesized `regex(pattern=…)` form), so the flat `regex=…` form — which stores its value under `params["value"]` — fell through to the match-everything default `.*`; the flat form now binds its value as the pattern.
- `http/server_sent_event.go` — `ServerSentEventWriter.Send` now treats a bare `CR`, a `CRLF`, or an `LF` inside `Data` as a data-line boundary (per the EventSource specification) instead of deleting bare carriage returns. A `CR`-delimited payload was previously merged into a single `data:` line, corrupting multi-line data carried across instances by the backplane; injection remains neutralized because each fragment stays a `data:` line.
- `security/` — `AccessControlBuilder.AllowAnonymous(prefix)` now matches on path-segment boundaries instead of a raw string prefix. `AllowAnonymous("/api/public")` previously also opened sibling paths such as `/api/public-data` and `/api/publicXYZ/secret` to anonymous access; it now uses the segment-aware rule so only `/api/public` and its `/`-delimited children are public.
- `security/` — `NewApiKeyHeaderAuthenticator` now panics on an empty expected value, matching `NewApiKeyHeaderRule`. An empty configured key produced an authenticator that could never authenticate (a misconfiguration that should fail loudly at construction rather than degrade silently).
- `security/` — a signed JWT or stored opaque token with an empty/absent/non-string subject is now rejected instead of authenticating as the empty principal `""`. `InMemoryTokenStore.Lookup`/`Put` copy the `Claims` slice and map fields so a caller mutating returned `Roles`/`Scope`/`Attributes` can no longer mutate the stored entry.
- `messagebus/` — when retries are exhausted and the `FailureTransport` itself rejects the message, the source requeue now carries a backoff `DelayStamp` so a persistently unreachable failure transport no longer spins at full speed. A delivery channel that closes without a cancelled context now returns an error from the consumer (a lost broker connection is no longer reported as a clean exit). The in-memory transport drops a requeue on a closed transport instead of blocking.
- `translation/` — `#` now stays bound to the enclosing `plural` through a nested `select` (ICU semantics); previously a `select` nested inside a `plural` lost the number substitution.
- `mailer/` — long header values are folded at whitespace per RFC 5322 (a long pure-ASCII subject no longer produces a single line over the 998-octet limit); the `Manager` validates every address with `net/mail.ParseAddress` (rejecting malformed/smuggled addresses before delivery); and a message with attachments but an empty body no longer emits a spurious empty `text/plain` part.
- `http/server_sent_event.go` — `ServerSentEventWriter.Send` strips `CR`/`LF` from the `Id` and `Event` fields so a caller-supplied value can no longer inject extra Server-Sent Events fields or events. The `Data` field is split into multiple `data:` lines on `LF` and now also has stray `CR` stripped from each line — a lone `CR` is an Server-Sent Events line terminator, so without this a `CR` embedded in `Data` could inject an extra field client-side; this matters now that the Server-Sent Events backplane carries `Data` across instances as JSON (which preserves `CR`). `ServerSentEventWriter.Comment` likewise strips `CR`/`LF` from its text (it shares the same field sanitizer as `Send`), so a dynamic comment can no longer break out of the `: ` comment line and inject an Server-Sent Events field.
- `http/server_sent_event_hub.go` — `Broadcast` no longer replicates to the `ServerSentEventBackplane` after `Shutdown`: once the hub is closed `DeliverLocal` already returns zero (its subscribers are drained), so a late broadcast was still publishing a cross-instance event no local subscriber wanted and attempting a publish during teardown. `replicate` now short-circuits when the hub is closed, reading the closed flag under the same lock it takes to snapshot the backplane.
- `openapi/` — a nullable pointer to a named struct now emits `{"allOf":[{"$ref":…}],"nullable":true}` instead of setting `nullable` as a sibling of `$ref`, which OpenAPI 3.0 tooling ignores (the field's nullability was being lost).
- `openapi/` — two distinct struct types that share a bare name (e.g. `product.Request` and `order.Request`) no longer collide in `components/schemas`: the component name was keyed on `reflect.Type.Name()` alone, so the second type silently reused the first type's schema and every `$ref` to it pointed at the wrong shape. Each distinct type is now assigned a unique component name (the clashing one is disambiguated with a numeric suffix).
- `lock/` — `InMemoryLocker` `Refresh` rejects a non-positive `ttl` (returning an error) instead of silently converting a TTL lock into a never-expiring one, matching the Redis backend's guard.
- `messagebus/` — the in-memory transport gains `WithLogger`; a delayed requeue dropped on a full/closed queue is now logged instead of vanishing silently.
- `lock/` — `InMemoryLocker` opportunistically purges expired holders during `Acquire`, bounding map growth for locks that expire without an explicit `Release`/`Refresh`.
- `security/` — runtime access-control matching now honors the rule type. `(*AccessControl).Match` already implemented the documented exact → longest-prefix → segment-prefix → regex → fallback priority, but the kernel access-control listener matched requests with a separate prefix-only matcher that ignored `isExact`/`isRegex`/`isSegmentPrefix`, so `NewAccessControlExactRule` was enforced as a prefix, `NewAccessControlRuleWithSegmentPrefix` lost its segment boundary, and `NewAccessControlRegexRule` rules (which carry an empty prefix) collapsed into the single empty-prefix fallback — letting a later protected regex rule be silently shadowed by an earlier public one. The listener now routes through `(*AccessControl).Match`, so a protected `^/admin` regex rule is enforced instead of a request falling through to an earlier `PUBLIC_ACCESS` rule.
- `messagebus/` — `melody:messagebus:consume --limit` no longer overshoots by up to `concurrency-1` messages: the limit budget is reserved before a worker receives an envelope, so a `--limit N --concurrency M` run consumes and acks exactly `N` messages instead of up to `N+M-1` (previously the limit was only checked after a message had already been dispatched and acked).
- `openapi/` — a route registered with multiple HTTP methods now emits a distinct `Operation` per verb with a unique `operationId` (`<routeName>.<method>`) instead of sharing one `Operation` pointer and a duplicate `operationId` across verbs, which is an invalid OpenAPI 3 document and aliased every verb's request/response shape.
- `openapi/` — promoted embedded-struct fields are now resolved by depth, matching `encoding/json`: the shallowest field with a given json name wins regardless of the embed's declaration order, and fields that tie at the minimum depth are dropped as ambiguous (unless exactly one is explicitly json-tagged). Previously the embed pass recursed depth-first with a first-wins skip, so a deeper-but-earlier embedded field could shadow a shallower-but-later one and the generated schema documented the wrong type for that field.
- `mailer/` — `foldHeaderLine` now folds a long opening token onto a continuation line too; previously the first word never folded, so a long single-token header value (a tracking id, a `List-Unsubscribe` URL) left the opening header line unbounded. An indivisible token longer than the limit is still emitted intact rather than split, since folding whitespace injected inside a token would corrupt its value once the receiver unfolds the header.
- `mailer/` — the SMTP transport now fails closed when `SmtpConfig{RequireAuth: true}` is set but no username is configured (for example a secret that resolves to the empty string): the `RequireAuth` check was previously gated on a non-empty username, so the transport skipped authentication entirely and delivered the message unauthenticated, making the control a silent no-op exactly when it mattered.
- `storage/` — `NewLocalStorage` no longer false-rejects every key when the base directory is created lazily under a symlinked ancestor (a common container/mount layout): the symlink-escape guard re-resolves the base each call instead of caching the unresolved path from construction time, so an object resolved through the symlink is correctly recognised as inside the base rather than reported as a symlink escape.
- `security/` — `AccessControlBuilder.AllowAnonymous(prefix)` now emits a rule carrying the `PUBLIC_ACCESS` attribute instead of an empty attribute set. An empty-attribute rule matched the path but then fell through the access-control listener's public-access check to the require-authentication branch, so `AllowAnonymous` actually **denied** anonymous requests (401) — the opposite of its name; routes meant to be public now grant access without a token.
- `openapi/` — wildcard route segments (`*name` single and `*name...` catch-all, both supported by the router) are now normalised to `{name}` in the generated path template and emit a path parameter, instead of being written verbatim. A `*` segment is not a valid OpenAPI 3.0.3 path key, so the previous output produced a document strict validators reject and left the wildcard parameter undocumented.
- `http/` — `JsonHandler[Req]` now rejects trailing data after the first JSON value (matching `Request.BindJson`'s whole-body semantics), returning `400 invalid json`. `json.Decoder.Decode` reads only the first value and silently ignored the rest, so a body such as `{…} garbage` or two concatenated objects was accepted while the manual binder rejected the same input.
- `storage/` — `LocalStorage` no longer follows a symlink at the leaf of a key. A leaf that is a symlink — including a *dangling* one whose target does not exist yet — was approved by the ancestor-only symlink walk, so an `O_CREATE` write followed the link and planted a file outside the base directory; the leaf is now rejected and `Put`/`Get` open with `O_NOFOLLOW` to also close the time-of-check/time-of-use window on a swapped leaf symlink.
- `storage/` — `LocalStorage.Put` now enforces the caller-declared content length: a reader that ends early or runs long returns an error (and the partial object is removed) instead of silently persisting a wrong-sized object, matching the S3 backend's content-length semantics. A negative size means "unknown" and skips the check.
- `security/` — `NewApiKeyHeaderRule` now panics on an empty header name or empty expected value instead of failing open. An empty expected key made a request that omits the header compare `""` against `""`, which the constant-time compare reports as equal — granting every unauthenticated request — mirroring the guard `ApiKeyHeaderAuthenticator` already had.
- `security/` — `NewAccessControlRule`/`NewAccessControlRuleWithSegmentPrefix` now reject `PUBLIC_ACCESS` combined with any other attribute. The listener grants on `PUBLIC_ACCESS` before any token/role/voter check, so a rule such as `(PUBLIC_ACCESS, ROLE_ADMIN)` would silently open the endpoint to everyone and discard the role requirement; the combination now fails closed at construction. The `AllowAnonymous` builder (a lone `PUBLIC_ACCESS`) is unaffected.
- `openapi/` — an optional path parameter (`:slug?`, supported by the router) is now normalised to `{slug}` with parameter name `slug` instead of the invalid template `{slug?}`/parameter name `slug?`; a bare unnamed placeholder (`*`, `:`, `{}`) gets a synthesised positional name instead of leaving the raw router token in the path key — both previously produced documents strict OpenAPI validators reject.
- `openapi/` — routes whose only methods are `HEAD`/`OPTIONS`/`TRACE` are no longer silently dropped from the document: `PathItem` gained `head`/`options`/`trace` and `assignOperation` now maps those verbs.
- `openapi/` — `greaterThan`/`lessThan` emit `minimum`/`maximum` only on numeric schemas and `regex`/`pattern` emits `pattern` only on string schemas, matching the existing `min`/`max` type guard, so a mis-applied `validate` tag can no longer emit a keyword that is invalid for the field's type.
- `messagebus/` — `Manager.Dispatch` rejects a `nil` message with a clear error instead of panicking. A nil message has no reflectable type, so the no-handler path dereferenced a nil `reflect.Type` (`reflect.TypeOf(nil).String()`) and crashed the dispatch/worker goroutine; the no-handler path also reports a safe `<nil>` type name as defence in depth.
- `messagebus/` — the `melody:messagebus:consume` retry-exhaustion path now logs a warning before nacking without requeue when no `FailureTransport` is configured, mirroring the decode-failure poison path, so an operator running the default (no dead-letter) no longer silently loses every message that exhausts its retries.
- `mailer/` — a long *no-space* ASCII `Subject` is now chunked into RFC 2047 encoded-words so every emitted line stays under the RFC 5322 / SMTP 998-octet hard limit. `mime.QEncoding` leaves pure-ASCII unchanged, so such a subject previously folded into one oversized line many MTAs reject or corrupt; adjacent encoded-words are concatenated without their separating whitespace on decode, so the subject round-trips.
- `mailer/` — a caller-supplied custom header now receives the same overlong-token protection as `Subject` (`encodeHeaderText`), so a value that is a single no-space token longer than the line limit (a tracking id, a signed `List-Unsubscribe` URL, a JWT) is chunked into RFC 2047 encoded-words instead of being emitted intact on one line over the 998-octet hard limit that strict MTAs reject. Normal short or whitespace-delimited header values are left byte-for-byte unchanged (`mime.QEncoding` is a no-op for pure-ASCII without an overlong token); only the pathological indivisible-token case is encoded, and it round-trips for RFC 2047-aware readers. Structural address headers still fold at whitespace via `foldHeaderLine`.
- `openapi/` — a `[]byte` field is now described as a base64 `{"type":"string","format":"byte"}` instead of an array of integers. `encoding/json` (used by both `JsonHandler` and `Request.BindJson`) serialises a byte slice as a base64 string, so the previous integer-array schema contradicted the framework's own wire format and a client generated from the spec would send a body the binder rejects. Fixed-size byte arrays (`[N]byte`), which `encoding/json` does not base64-encode, stay integer arrays.
- `mailer/` — `Manager.Send` now rejects a message whose only recipients carry an empty email address. The recipient-presence guard counted slot lengths, but `validateAddresses` skips empty-email entries, so a recipient slot with only a display name passed validation and reached the transport with no deliverable recipient (silently recorded by transports that trust the manager's validation); the guard now counts addresses with a non-empty email.
- `openapi/` — two of a struct's **own** (non-embedded, depth-0) fields that map to the same json name are now resolved like `encoding/json`: the conflict is kept only when exactly one of them is explicitly json-tagged, otherwise both are dropped as ambiguous. The own-field pass was first-wins, so it emitted a property for a json name that `encoding/json` (used by `JsonHandler`/`Request.BindJson`) never serializes or accepts — diverging the generated spec from the wire format. The embedded-field pass already applied this rule; the own-field pass now shares it.
- `http/server_sent_event.go` — `ServerSentEventWriter.Send` no longer emits an empty `data:` line for an event that carries no `Data` (an id-only or retry-only event). A data field — even empty — makes an `EventSource` client dispatch a spurious empty message, whereas an event with only `Id`/`Retry` must update the last-event-id / reconnection time without dispatching; the `data` block is now guarded for an empty `Data`, matching the existing empty-guards on the `Id`/`Event`/`Retry` fields.
- `session/` — `InMemoryStorage.Load` no longer deletes a freshly-saved session when an expired entry is evicted under concurrency. The expiry path released the read lock and then deleted the key unconditionally under the write lock, so a `Save` that landed in that window wrote a fresh entry the stale delete immediately removed; `Load` now re-checks, under the write lock, that the current entry is still expired before deleting it (the same pattern the cache backend already uses).
- `validation/` — `greaterThan`/`lessThan` now reject a floating-point `NaN` instead of silently accepting it. IEEE-754 comparisons against `NaN` are always false, so `NaN >= max` / `NaN <= min` evaluated false and the value passed the bound; the constraints now reject a non-finite float explicitly.
- `session/` — `InMemoryStorage` `Load`/`Save` now deep-copy nested `map[string]any` values (via the same `copyAnyMap` the file backend already uses) instead of copying only the top-level map. A caller mutating a nested map returned by `Load` (or one retained after `Save`) could previously reach through the shared reference and corrupt the stored session; the in-memory and file backends now isolate session data identically.
- `messagebus/` — `NewSendMessageMiddlewareFromRouting` now snapshots the `Routing` map when the middleware is built rather than sharing the builder's live map. A `RouteType` call made after the middleware was constructed could concurrently mutate the map the dispatch path reads (a `fatal error: concurrent map read and map assignment`); the built middleware now holds an isolated copy.
- `messagebus/` — the in-memory transport's `WithLogger` and the delayed-requeue goroutine that reads the logger are now synchronized, removing a data race when a logger is attached while a delayed requeue is in flight (matching the audit storage's logger guard).
- `storage/` — `LocalStorage.Exists` now uses `os.Lstat` instead of `os.Stat`, so it never follows a symlink at the object path. `Put`/`Get` already open with `O_NOFOLLOW` and `resolvePath` rejects a symlinked leaf; `Exists` now matches that no-follow posture, closing a narrow window where a symlink swapped in after the path check could have disclosed the existence of a target outside the base directory.

## [v3.6.0] - 2026-05-16 - Cron Integration, Decoupled Cron Configuration, and `.example` Flat Layout

### Added

- `cli/contract/type.go` — `StringSliceFlag` type alias for `urfavecli.StringSliceFlag`; lets commands declare repeatable string-slice flags (consumed by `integrations/cron/v3` for `--heartbeat-command` and `--heartbeat-destination`) via `clicontract.StringSliceFlag` like every other flag type
- `.documentation/package/CLI.md` — listed `clicontract.StringSliceFlag` in the package surface and added a pointer to `integrations/cron/v3/` for users looking for a crontab generator
- `v3/.example/go.mod` — `v3/.example/` is now a standalone Go module (`github.com/precision-soft/melody/v3/.example`) so it can `require` framework integrations (such as `integrations/cron/v3`) without creating a cycle with the framework's own `go.mod`; local `replace` directives keep workspace builds resolving against the in-tree melody and integrations/cron checkouts
- `v3/.example/config/` package — formerly `v3/.example/bootstrap/`, now flat-layout; each Module hook lives in its own file with a matching compile-time interface assertion at the bottom (`module.go` → `Module`, `parameter.go` → `ParameterModule`, `service.go` → `ServiceModule`, `security.go` → `SecurityModule`, `event.go` → `EventModule`, `middleware.go` → `HttpMiddlewareModule`, `http.go` → `HttpModule`, `cli.go` → `CliModule`, plus `cron.go` for the cron registry helper and `configure.go` for the entry point)
- `v3/.example/config/parameter.go` — registers cron parameters (`melody.cron.user`, `melody.cron.heartbeat_path`, `app.cron.product_user`, …) from `APP_CRON_*` env vars so the example demonstrates the env-driven cron configuration pattern
- `v3/.example/config/cron.go` — extracts the cron `Configuration` build into a dedicated helper (`newCronConfiguration(kernel)`) that reads `app.cron.product_user` from the parameter cascade and applies it as a per-command `User` on the `product:list` schedule; pedagogical demonstration of how `.env` → `RegisterParameter` → `kernel.Config().Get(...)` → `cron.EntryConfig` flow works end-to-end
- `v3/.example/config/cli.go` — `RegisterCliCommands` returns the CLI command list plus `melody:cron:generate` constructed from `newCronConfiguration(kernelInstance)`
- `v3/.example/config/service.go` — services are now registered through `(*Module).RegisterServices(registrar)` implementing `applicationcontract.ServiceModule` (instead of a top-level `registerServices(app)` function called from `Configure`)
- `v3/.example/config/middleware.go` — HTTP middleware is now registered through `(*Module).RegisterHttpMiddlewares(kernel, registrar)` implementing `applicationcontract.HttpMiddlewareModule` (instead of a direct `app.RegisterHttpMiddlewares(NewTimingMiddleware())` call from `Configure`); `NewTimingMiddleware` factory is retained
- `v3/.example/config/configure.go` — simplified to a single `app.RegisterModule(NewExampleModule())` call now that every Module* interface is implemented on `*Module` directly
- `v3/.example/security/default_access_denied_handler.go`, `v3/.example/security/login_redirect_entry_point.go` — added compile-time interface assertions (`var _ AccessDeniedHandler = ...`, `var _ EntryPoint = ...`)
- `application/application_new.go` — `computeProjectDirectory` now prefers the working directory over the closest `go.mod` ancestor when the working directory itself contains `.env` or `.env.local`. This unblocks `go run .` for sub-applications whose `.env` lives next to `main.go` rather than at the parent module's root
- `application/application_test.go` — `TestWorkingDirectoryHasEnvironmentFile_*` covers the new `.env` / `.env.local` detection helper
- `http/exception_listener_test.go`, `http/test_helper_test.go` — backfilled from v1 (introduced in v1.10.1 but never propagated) so the kernel exception listener's HTML XSS escaping, debug-mode message handling, request-id header, and existing-response preservation are now covered on v3 as well

### Changed

- `http/accept.go` — `PrefersHtml` refactored to short-circuit when `text/html` is absent from the `Accept` header, skipping the `application/json` scan and reducing the common-case complexity from O(2N) to O(N); v1/v2/v3 implementations are now byte-identical apart from the melody import path
- `logging/default_logger.go` — rename abbreviated loop variables `i` and `v` to `index` and `value` in `joinPairs`
- `http/response.go` — rename abbreviated loop and parameter variables `r`, `b` to `runeChar`, `byteChar` in `asciiFallbackFilename`, `rfc5987EncodeFilename`, and `isRfc5987AttrChar`
- `v3/.example/` — flattened `domain/` and `infra/` layers into top-level packages (`cache/`, `cli/`, `entity/`, `event/`, `handler/`, `page/`, `presenter/`, `repository/`, `route/`, `security/`, `service/`, `subscriber/`, `url/`). Renamed `bootstrap/` to `config/`. Flat layout. Domain and in-memory repositories collapsed into a single `repository/` package
- `v3/.example/.env` — adds `APP_CRON_USER`, `APP_CRON_HEARTBEAT_PATH`, and `APP_CRON_PRODUCT_USER` so the cron default user, heartbeat path, and `product:list` per-command user are sourced from the environment rather than hard-coded
- `v3/.example/.gitignore` — ignores `/generated_conf/` (output directory for `melody:cron:generate`)
- `v3/.example/README.md` — documents the new flat layout, the cron `Configuration` registry, the env-driven cron parameters, and `melody:cron:generate` usage
- `go.work` — register the new `.example/`, `v2/.example/`, `v3/.example/` workspace modules

### Removed

- `v3/.example/bootstrap/`, `v3/.example/domain/`, `v3/.example/infra/` — flattened into top-level packages (see "Changed")
- `v3/.example/.example/` — duplicated nested copy of the example tree committed in error during the v3 bootstrap (commit `4e52f40`). Go silently skipped it because of the dot-prefix, so removing it has no behavioral impact

## [v3.5.0] - 2026-04-20 - Harden HTTP Server Timeouts and Align RunHttp Signature

### Changed

- `application/application_http.go` — `runHttp` now accepts a caller-supplied `context.Context` first parameter and observes it for shutdown instead of reading the receiver's stored `ctx` field. Aligns v3 with the v1/v2 `runHttp(ctx)` signature (MEL-171)

### Added

- `application/application_http.go` — HTTP server now sets hardened timeout defaults (`ReadTimeout=15s`, `ReadHeaderTimeout=5s`, `WriteTimeout=30s`, `IdleTimeout=60s`, `MaxHeaderBytes=1MiB`) to defend against slowloris / slow-body attacks on exposed servers (MEL-148)
- `application/application_http_timeouts.go` — new optional `HttpTimeoutConfiguration` interface; any `HttpConfiguration` that implements it can override the hardened defaults per timeout without breaking existing configurations (MEL-148)
- `application/application_http_timeouts_test.go` — coverage for default application and interface-driven overrides

## [v3.4.0] - 2026-04-17 - Extract HTTP CORS Subpackage and Harden Request Lifecycle

### Changed

- `http/middleware/cors.go` — public CORS API (`CorsConfig`, `NewCorsConfig`, `DefaultCorsConfig`, `RestrictiveCorsConfig`, `CorsMiddleware`, `DefaultCorsMiddleware`, `RestrictiveCors`) moved to `http/cors/`. Old symbols retained in `http/middleware/` as deprecated shims that delegate to `http/cors`; kept for backwards compatibility, no removal scheduled
- `http/middleware/compression.go` — gzip now streams through `io.Pipe` instead of buffering the full body; `Vary: Accept-Encoding` is always emitted; `Accept-Encoding` parsing uses RFC 7231 q-values via `acceptsGzip` (explicit `gzip;q=0` is respected)
- `http/middleware/rate_limit.go` — default `keyExtractor` is now built inside `RateLimitMiddleware` from the configured `ClientIpResolver`; `SimpleRateLimit`/`IpRateLimit` no longer embed the extractor directly
- `http/kernel.go` — incoming request bodies are wrapped with `net/http.MaxBytesReader` when `kernel.http.max_request_body_bytes` is positive; discarded responses replaced by an error handler are now closed via `closeDiscardedResponseBody` to avoid leaking file descriptors / connections
- `container/scope.go` — `scope.container` is now `atomic.Pointer[container]`; `Close` nils the pointer so a concurrent `Get`/`Resolve` returns a clean "scope closed" error instead of racing on a nil deref
- `cache/in_memory.go` — removed `runtime.SetFinalizer` fallback and the `cleanupCancel`/`context.Context` path; cleanup goroutine now terminates solely via `Close`/`stopCleanup`, documented as owner-closed
- `logging/json_logger.go` — writes are serialized through `sync.Mutex` so concurrent `Log` calls produce cleanly separated JSON lines on the shared writer
- `security/api_key_authenticator.go` — credential comparison switched to `crypto/subtle.ConstantTimeCompare` to eliminate the timing-leak on API key length/prefix matches
- `session/file_storage.go` — file writes are now atomic (`os.CreateTemp` + `os.Rename`) instead of truncate-in-place; load path decoupled from a long-lived `*os.File` handle; `ownsFile` retired in favor of path-based ownership
- `.documentation/package/*.md` — full documentation overhaul across APPLICATION/CACHE/CLI/CONFIG/CONTAINER/EVENT/HTTP/HTTPCLIENT/LOGGING/SECURITY/SESSION/VALIDATION: added missing userland types, constructors, container-access helpers, environment key tables, constants, and footgun notes

### Added

- `http/cors/` — new subpackage extracted from `http/middleware/cors.go`. Split into `cors.Service`, `cors.Middleware`, and `cors.RegisterResponseListener` so CORS headers are applied both on the happy path (middleware) and on error-path responses produced by the kernel (`kernel.response` listener, priority `-100`)
- `http/response.go` — `BuildContentDisposition(disposition, filename)` emits RFC 6266 `Content-Disposition` with both `filename="..."` ASCII fallback and `filename*=UTF-8''...` RFC 5987 encoding for non-ASCII filenames; `AttachmentResponse` now routes through it
- `http/middleware/rate_limit.go` — `ClientIpResolver` hook and `DefaultClientIp` for proxy-aware IP resolution; `RateLimitConfig.SetClientIpResolver(...)` lets userland install X-Forwarded-For / X-Real-IP strategies without rewriting key extractors
- `http/request.go` — form auto-parsing now gated on `Content-Type` (`application/x-www-form-urlencoded` or `multipart/form-data`); JSON/XML/binary bodies are no longer consumed by `NewRequest`
- `session/session.go` — `isValidSessionId` enforces 32-char lowercase-hex format; `Manager.Session`/`DeleteSession` reject malformed cookies before hitting storage
- Test coverage: `http/cors/{listener,middleware,service}_test.go`, `http/request_test.go`, `http/response_test.go`, `container/scope_test.go` concurrent Close/resolve test, `logging/json_logger_test.go` concurrent writes, `session/file_storage_test.go` atomic write and reopen coverage

### Deprecated

- `http/middleware.CorsConfig`, `http/middleware.NewCorsConfig`, `http/middleware.DefaultCorsConfig`, `http/middleware.RestrictiveCorsConfig`, `http/middleware.CorsMiddleware`, `http/middleware.DefaultCorsMiddleware`, `http/middleware.RestrictiveCors` — use the equivalents in `github.com/precision-soft/melody/v3/http/cors` instead. Deprecated symbols are kept for backwards compatibility; no removal scheduled.

## [v3.3.1] - 2026-04-17 - Fix Compression Error Propagation and Concurrent Access Races

### Fixed

- `http/middleware/compression.go` — compression middleware now propagates `io.ReadAll` errors instead of silently returning partial data to the client; simplified level validation to single `HuffmanOnly`/`BestCompression` bounds check
- `http/static/utility.go` — static file server now validates resolved symlink targets via `filepath.EvalSymlinks()` and returns 403 for paths escaping the configured root directory; `EvalSymlinks` errors are now propagated directly instead of being mapped to `fs.ErrNotExist`
- `config/configuration.go` — placeholder regex now requires identifiers to start with a letter or underscore, rejecting patterns like `%1invalid%`
- `config/configuration_resolve.go` — fix shadowed `err` variable in `resolveSinglePass` that silently discarded template resolution errors
- `session/file_storage.go` — `flushToFile` no longer redundantly reloads the file after a successful rename-based swap
- `logging/logger.go` — `LogError()` nil-logger check moved after the fallback `log.Printf` path so `AlreadyLogged` is only evaluated when a logger is present
- `session/in_memory_storage.go` — `Load()` now holds `RLock` during the data copy to prevent a race with concurrent `Save()` calls
- `session/file_storage.go` — `Load()` now holds `RLock` during `copyAnyMap()` to prevent a race with concurrent `Save()` calls
- `httpclient/http_client.go` — `SetTimeout()` no longer mutates `http.Client.Timeout` on the shared client (which races with in-flight `Do()` calls); `clientForRequest` now reads the instance timeout under `RLock` and builds a per-request client only when it differs from the shared client's construction timeout
- `logging/emergency_logger.go` — `CloseEmergencyLogger()` now resets the singleton to `nil` so that subsequent `EmergencyLogger()` calls actually create a fresh instance (previously the closed instance was retained)

### Changed

- `httpclient/http_client.go` — added `sync.RWMutex` to protect concurrent access to `baseUrl`, `headers`, and `timeout` fields
- `httpclient/http_client_config.go` — `Headers()` now returns a defensive copy of the map
- `cli/output/application_version.go` — application version storage replaced with `sync/atomic.Value` for thread safety
- `logging/emergency_logger.go` — replaced `sync.Once` with `sync.Mutex` so `CloseEmergencyLogger()` can reset the singleton and a subsequent `EmergencyLogger()` call creates a fresh instance
- `http/kernel.go` — `debugMode` variable hoisted to single computation at request entry
- `application/application_http.go` — extracted `httpShutdownTimeout` constant for the HTTP server shutdown deadline
- `cache/in_memory.go` — removed redundant map copy in `SetMultiple`

### Added

- `http/static/utility_test.go` — symlink traversal rejection, absolute path rejection, parent traversal rejection, symlink within root allowed
- `cli/output/application_version_test.go` — Set/Get coverage and concurrent access race test
- `logging/emergency_logger_test.go` — singleton behavior, `Close`/recreate cycle, concurrent access
- `httpclient/http_client_test.go` — concurrent `SetHeader`/`SetBaseUrl`/`SetTimeout` with in-flight requests, `HttpClientConfig.Headers()` defensive copy
- `http/middleware/compression_test.go` — HuffmanOnly and BestCompression level boundary acceptance, out-of-range fallback to DefaultCompression
- `config/configuration_test.go` — placeholder regex rejects identifiers starting with digits, accepts letter/underscore/dotted identifiers
- `session/in_memory_storage_test.go`, `session/file_storage_test.go` — concurrent `Load`/`Save` race tests

## [v3.3.0] - 2026-04-14 - Improve Goroutine Lifecycle and Default Logger

### Changed

- `cache/in_memory.go` — `cleanupLoop` accepts `context.Context`; `NewInMemoryCache` creates a cancel context stored as `cleanupCancel`; `Close()` calls `cleanupCancel()` to stop the goroutine cooperatively
- `session/in_memory_storage.go` — same goroutine lifecycle improvements as `cache/in_memory.go`
- `http/request.go` — replace `log.Printf` fallback (when no runtime instance is available) with `logging.NewDefaultLogger().Warning(...)`; remove unused `"log"` import
- `cli/command.go` — remove block comments and `//nolint:errcheck` directives from `printGreenFullLine`, `printGreenStatusLine`, `printRedStatusLine` closures
- `logging/logger.go` — add GoDoc comment to `causeChainMaxDepth` constant
- `security/compiled_configuration.go` — group string fields in `CompiledFirewall` struct (`name`, `matcherDescription`, `loginPath`, `logoutPath`)

## [v3.2.0] - 2026-04-13 - Fix Validators, Rate Limiter, and Router

### Fixed

- `validator.go` — `createConstraintWithParams` now handles `greaterThan` parameters; `validate:"greaterThan(value=5)"` was silently using `min=0`
- `rate_limit.go` — `getClientIp` strips port via `net.SplitHostPort`; rate limiting was per-connection instead of per-IP
- `url_generator.go` — path parameters now URL-encoded via `url.PathEscape`; special characters produced malformed URLs
- `accept.go` — `PrefersHtml` uses position-based comparison; browsers sending both `text/html` and `application/json` now correctly get HTML
- `compression.go` — `gzip.NoCompression` (level 0) is no longer overridden to default compression
- `constraint_greater_than.go` — added `float32`/`float64` support; float values no longer return "value must be an integer"
- `kernel.go` — `errorHandler` now called for controller errors (was only called on panic recovery path)
- `kernel.go` — logger creation failure returns 500 response instead of panicking
- `cors.go` — panic at middleware initialization when `AllowCredentials=true` and origins contain `"*"` to prevent overly permissive CORS

### Changed

- `file_storage.go` — `copyAnyMap` performs recursive deep copy for nested `map[string]any` values
- `exception/utility.go` — export `BuildCauseChain` and `BuildCauseContextChain`
- `logging/logger.go` — remove duplicated cause chain functions; delegate to `exception.BuildCauseChain` / `exception.BuildCauseContextChain`
- `router_utility.go` — remove implicit HEAD-to-GET match from `matchesMethod`; kernel `HeadFallbackToGet` policy is now the single control point
- `config/configuration.go` — `RegisterRuntime` is now thread-safe via `sync.Mutex`
- `http/static/file_server.go` — extract shared path-resolution and cache logic into `resolveAndOpen` helper; `Serve` and `serveForStreaming` both delegate to it

## [v3.1.4] - 2026-04-10 - Fix XSS, Symlink Traversal, and Routing Edge Cases

### Fixed

- `exception_listener.go` — HTML error response now escapes error messages with `html.EscapeString` preventing XSS
- `exception_listener.go` — use `LoggerFromRuntime` instead of `LoggerMustFromRuntime` to prevent panic when runtime logger is not available
- `router_utility.go` — wildcard locale route attribute used `RouteAttributeName` instead of `RouteAttributeLocale`, causing catch-all wildcards named `_route` to incorrectly write to the `_locale` param
- `middleware/compression.go` — `ReadAll` error discarded partially read data; now preserves whatever was read before the error
- `middleware/cors.go` — origin matching was case-sensitive; now uses `strings.EqualFold` for case-insensitive comparison
- `middleware/rate_limit.go` — `getClientIp` now uses `RemoteAddr` only; ignores `X-Forwarded-For` and `X-Real-IP` headers to prevent IP spoofing

### Changed

- `kernel.go` — remove dead nil checks on `MatchResult` (router `Match()` always returns non-nil)
- `request.go` — log warning when `ParseForm` fails (previously silent)
- `url_generation_route_definition.go` — `Defaults()` and `Requirements()` now return defensive copies
- Rename `security/security_test.go` to `security/test_helper_test.go`
- Remove redundant comments from modified files

### Added

- `request_test.go`, `middleware/compression_test.go`, `middleware/cors_test.go`, `url_generation_route_definition_test.go` — new and expanded test coverage for all fixes

## [v3.1.3] - 2026-03-21 - Refactor Address Colon Check in Config

### Changed

- `config/http.go` — replaced colon-based address check with `strings.Contains` for correct host:port detection

## [v3.1.2] - 2026-03-18 - Fix HTTP HEAD Handling and Update Dev Scripts

### Fixed

- `http/router_utility.go` — aligned HEAD handling and response contract validation; prevents incorrect responses on HEAD requests

### Changed

- `internal/reflect.go` — updated type-reflection utilities

## [v3.1.1] - 2026-03-17 - Fix JSON Logging Level Label Preservation

### Fixed

- `logging/contract/level.go`, `logging/logger.go` — preserved numeric logging level labels in JSON output; `logging/json_logger_test.go` — coverage

## [v3.1.0] - 2026-03-17 - Add Module Configuration Registration and Logging Labels

### Added

- `application/contract/config_module.go` — new `ConfigModule` interface allowing modules to register configuration during application boot
- `logging/contract/config.go`, `logging/logging_config.go` — `LoggingConfig` struct and contract for customizable logging level labels
- `logging/default_logger.go`, `logging/json_logger.go`, `logging/logger.go` — updated to apply level label customization from `LoggingConfig`
- `application/application.go`, `application/application_module.go`, `application/application_new.go` — wired `ConfigModule` into the application boot sequence

## [v3.0.1] - 2026-03-08 - Lock-Step Release — Align Integration Module Dependencies

Lock-step release — no `v3/` changes this cycle. Tag SHA differs from `v3.0.0` only through integration-module-scoped files; the `v3/` tree is unchanged. See the integration CHANGELOGs for the actual content.

## [v3.0.0] - 2026-03-08 - Introduce Melody v3 Module

### Added

- Introduce Melody v3 module (`github.com/precision-soft/melody/v3`)
- `application/application.go`, `application/application_new.go` — application context in constructor; `New(ctx context.Context, ...)` takes a caller-supplied context used for the full application lifecycle
- `application/contract/service_module.go` — `ServiceModule` simplification; single `Register(container)` method replacing split register/configure lifecycle

[Unreleased]: https://github.com/precision-soft/melody/compare/v3.7.0...HEAD

[v3.7.0]: https://github.com/precision-soft/melody/compare/v3.6.0...v3.7.0

[v3.6.0]: https://github.com/precision-soft/melody/compare/v3.5.0...v3.6.0

[v3.5.0]: https://github.com/precision-soft/melody/compare/v3.4.0...v3.5.0

[v3.4.0]: https://github.com/precision-soft/melody/compare/v3.3.1...v3.4.0

[v3.3.1]: https://github.com/precision-soft/melody/compare/v3.3.0...v3.3.1

[v3.3.0]: https://github.com/precision-soft/melody/compare/v3.2.0...v3.3.0

[v3.2.0]: https://github.com/precision-soft/melody/compare/v3.1.4...v3.2.0

[v3.1.4]: https://github.com/precision-soft/melody/compare/v3.1.3...v3.1.4

[v3.1.3]: https://github.com/precision-soft/melody/compare/v3.1.2...v3.1.3

[v3.1.2]: https://github.com/precision-soft/melody/compare/v3.1.1...v3.1.2

[v3.1.1]: https://github.com/precision-soft/melody/compare/v3.1.0...v3.1.1

[v3.1.0]: https://github.com/precision-soft/melody/compare/v3.0.1...v3.1.0

[v3.0.1]: https://github.com/precision-soft/melody/compare/v3.0.0...v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/compare/v2.1.3...v3.0.0
