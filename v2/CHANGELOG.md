# Changelog

All notable changes to `precision-soft/melody/v2` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v2.8.1] - 2026-06-25 - Cross-Version Security and Correctness Back-ports

### Fixed

- `internal/copy.go` — the session deep-copy (`CopyAnyMap`/`CopyAnySlice`, reached through the public `Session.Set`/`Save` API which take `any`) recursed into nested maps and slices with no depth bound, so a cyclic value (for example a map that contains itself) recursed until the goroutine stack overflowed — a fatal error no deferred `recover()` can catch, taking down the whole process. The recursion is now depth-bounded (returning the value as-is at the bound), which both halts a cyclic structure and leaves legitimate, far-shallower data fully deep-copied. Fixed in lockstep with `v1`/`v3`.
- `session/file_storage.go` — `FileStorage.Save`/`Delete` mutated the in-memory `sessionById` map before flushing and did not undo the change when the flush failed, so a `Save`/`Delete` that returned an error was still observable through a later `Load` in the same process (and diverged from the on-disk state after a restart). The in-memory entry is now rolled back on a flush failure, keeping the returned error consistent with both the in-memory and persisted state. Fixed in lockstep with `v1`/`v3`.
- `config/configuration.go` — `Configuration.Get`/`MustGet`/`Names`/`Parameters` read the shared `parameters` map without holding the lock that `RegisterRuntime` takes to write it, so calling `RegisterRuntime` (exposed at runtime via `kernel.Config()`) concurrently with any of those readers tripped Go's non-recoverable `fatal error: concurrent map read and map write`. The mutex is now a `sync.RWMutex`, the readers take the read lock, and `RegisterRuntime` uses the lock-free `getInternalParameter` internally to avoid a self-deadlock — completing the write-side guard added previously. Fixed in lockstep with `v1`/`v3`.
- `http/kernel.go` — when an `EventKernelResponse` listener replaced the response via `SetResponse`, the kernel wrote the new response but never closed the discarded original's body, leaking an open file descriptor for a file-backed response (`FileResponse` or static `ServeReader`). Each of the four response-dispatch sites now closes the discarded response body when the listener swapped it, matching the cleanup the error-handler swap paths already perform. Fixed in lockstep with `v1`/`v3`.
- `session/file_storage.go` — `writeSessionFileInPlace` (used by a `FileStorage` built from an injected `*os.File` via `NewFileStorageFromFile`) seeked and `Truncate(0)`-d the live file *before* JSON-encoding the session snapshot, so a `Save` whose value cannot be marshaled (for example a session value set to a channel or function — `Session.Set` takes `any`, and the value is only marshaled at flush time) left the file truncated to zero bytes, permanently destroying every previously-persisted session on disk while merely returning an error. It now encodes into an in-memory buffer first and only seeks, truncates and writes once the encode has succeeded, mirroring the validate-before-commit guarantee of the atomic `writeSessionFileAtomically` path. Fixed in lockstep with `v1`/`v3`.
- `container/scope.go` — `scope.MustGetByType(nil)` panicked with an obscure `runtime error: invalid memory address or nil pointer dereference` (and discarded the wrapped `GetByType` cause) instead of the intended descriptive panic: `GetByType(nil)` returns a clean "service type is required" error without dereferencing the type, but the error-reporting branch then called `String()` on the nil `reflect.Type`. It now guards the nil type when building the panic context, matching the sibling `resolverContext.MustGetByType` that already does. Fixed in lockstep with `v1`/`v3`.
- `security/config/access_control_builder.go` — `AllowAnonymous` matched its path prefix with a plain string prefix, so `AllowAnonymous("/api/public")` also opened sibling paths that merely share the string prefix (`/api/public-data`, `/api/publicXYZ/secret`) to unauthenticated access. It now builds the public-access rule with `NewAccessControlRuleWithSegmentPrefix`, matching only on a path-segment boundary (the declared prefix itself and its children). Ported from the `v3` fix.
- `security/api_key_authenticator.go` — `NewApiKeyHeaderAuthenticator` validated only the header name; an empty expected value constructed successfully even though it can never authenticate (a non-empty header never `ConstantTimeCompare`-equals `""`), a defensive gap relative to the sibling `ApiKeyHeaderRule`. It now panics on an empty expected value as well. Ported from the `v3` fix.
- `session/file_storage.go`, `session/in_memory_storage.go`, `internal/copy.go` — the session deep-copy recursed only into `map[string]any` and `[]any`, so any other typed collection stored in a session (e.g. `[]string`, `map[string]int`, `[][]string`) was copied by reference and could be mutated across loads/saves, leaking state between requests. The copy now lives in `internal.CopyAnyMap`/`CopyAnySlice` and deep-copies typed slices and maps reflectively. Ported from the `v3` fix.
- `validation/validator.go` — the `regex=<pattern>` shorthand form stores the pattern under the `value` key, but `createConstraintWithParams` only consulted the `pattern` key and otherwise fell back to `NewRegex(".*")`, which matches anything — a fail-open validation bypass for every shorthand regex rule. It now also honors the `value` key. Ported from the `v3` fix.
- `validation/validation_rule.go` — `splitByTopLevelComma` tracked only parenthesis depth, so a top-level comma inside a regex character class (`regex=^[a,b]$`) or quantifier (`regex=^a{1,2}$`) was mistaken for a rule separator, turning a valid tag into a broken regex plus a bogus "unknown validation rule". It now also tracks character-class and curly-brace state, matching the parenthesized form. Ported from the `v3` fix.
- `container/container.go` — `OverrideProtectedInstance` wrote the overridden value into the by-type instance map even for a service registered `WithoutTypeRegistration()`, creating a phantom type alias that caused a non-comparable value-type service with a value-receiver `Close` to be closed twice at shutdown. The by-type write is now gated on the type actually being registered. Fixed in lockstep with `v3`.
- `http/request_body.go` — `BindJson` reported an over-limit body as `400 Bad Request` instead of `413 Request Entity Too Large`: the kernel's `MaxBytesReader` returns its error before the local `LimitReader` cap is reached, so the oversize branch never fired on the normal request path. It now detects `*http.MaxBytesError` and returns `413`. Fixed in lockstep with `v1`/`v3`.
- `config/configuration.go` — `RegisterRuntime` performed an unguarded check-then-write on the shared `parameters` map, so two goroutines registering runtime parameters concurrently (or one registering while `Names()`/`Parameters()` iterated the map) raced on the map and could trigger Go's fatal "concurrent map writes". The read-modify-write is now serialized with a `sync.Mutex`, matching the `v3` field. Ported from the `v3` fix.
- `validation/constraint_greater_than.go` — the non-numeric fallback of `GreaterThan.Validate` reported `"value must be an integer"`, but the constraint accepts integer, unsigned, and floating-point values, so the message misled callers passing a valid float. It now reports `"value must be numeric"`, matching the `v3` wording. Ported from the `v3` fix.
- `logging/json_logger.go` — the `Log` marshal-failure fallback recomputed `time.Now()` for its `time` field instead of reusing the timestamp already captured for the primary entry, so a context value that fails to JSON-encode (for example a channel or function) produced a fallback line whose timestamp could drift from the moment the entry was created. The timestamp is now captured once and reused by both the primary entry and the fallback. Fixed in lockstep with `v1`/`v3`.
- `container/resolver.go` — `MustFromResolverByType` returned a nil value instead of panicking when a `Resolver` resolved the requested type to a nil pointer/interface, violating the `Must*` non-nil contract that the sibling `MustFromResolver` already enforces (a custom `containercontract.Resolver` whose `GetByType` returns `(typed-nil, nil)` slipped a nil through to the caller). It now applies the same `internal.IsNilInterface` guard and panics. Fixed in lockstep with `v1`/`v3`.
- `security/access_control.go` — `NewAccessControlRuleWithSegmentPrefix` (used by `AccessControlBuilder.AllowAnonymous`) accepted an empty path prefix, which normalized to `""` and became a catch-all fallback rule — so `AllowAnonymous("")` silently granted `PUBLIC_ACCESS` to every otherwise-unmatched path (fail-open). It now panics on an empty prefix, matching the existing empty-input guards on the exact and regex rule constructors; a fully public service declares an explicit `"/"` prefix. Fixed in lockstep with `v1`/`v3`.
- `validation/validator.go`, `validation/validation_rule.go` — a malformed numeric constraint parameter (for example `validate:"greaterThan(value=abc)"` or `validate:"min(value=notanumber)"`) silently degraded to the constraint's default bound instead of being reported, so a typo'd tag enforced a bound the author never specified (a fail-open configuration). Constraint creation now parses the value strictly (`parseIntStrict`) and a field whose numeric parameter cannot be parsed fails validation with the `invalidRuleSyntax` code instead. A valid leading integer is still accepted, so `max(value=3.9)` keeps truncating to `3`. **Behavioral note:** a previously-silent bad numeric tag now surfaces as a validation error. Ported from the `v3` fix.

## [v2.8.0] - 2026-06-16 - Configurable Transport & Shutdown Tunables + v3 Security and Correctness Back-ports

### Security

- `security/access_control_listener.go` — the access-control listener (the request authorization gate) matched only prefix rules and the empty-prefix fallback, silently ignoring exact (`NewAccessControlExactRule`) and regular-expression (`NewAccessControlRegexRule`) rules; a request could therefore bypass an exact or regular-expression access-control rule entirely. `matchAccessControlRule` now delegates to `AccessControl.matchRuleIndex`, sharing the full exact → prefix → regular-expression → fallback precedence already used by `AccessControl.Match`
- `security/rule.go` — `ApiKeyHeaderRule.Check` compared the configured key against the request header with a plain `==`, which is not constant-time and leaks key length and shared prefix through timing; the comparison now uses `crypto/subtle.ConstantTimeCompare`. `NewApiKeyHeaderRule` additionally panics when the header name or the expected value is empty, closing a fail-open path where a request that omits the header (yielding `""`) would compare equal to an empty expected key and authorize every caller
- `security/access_control.go` — `NewAccessControlRule` and `NewAccessControlRuleWithSegmentPrefix` now reject a rule that combines `PUBLIC_ACCESS` with any other attribute (via `normalizeAccessControlAttributes`); the listener grants `PUBLIC_ACCESS` before any role or voter check, so a rule such as `(PUBLIC_ACCESS, ROLE_ADMIN)` would have silently opened the endpoint to everyone and discarded the role requirement
- `security/config/access_control_builder.go` — `AllowAnonymous` appended a rule with no attributes, which the listener treats as "authentication required", so the helper actually denied anonymous access with a 401; it now carries `securitycontract.AttributePublicAccess` so anonymous requests are granted as intended
- `security/access_control.go` — an exact or anchored-regex access-control rule could be bypassed by appending extra trailing slashes (`/admin//` routes to the `/admin` handler, but `matchRuleIndex` trimmed only one trailing slash and so failed to match the exact `/admin` rule, leaving the request unguarded). `matchRuleIndex` now collapses all trailing slashes like the router. Ported from the `v3` fix.

### Added

- `security/rule_test.go` — regression coverage for the API-key rule fail-open guards (empty header name and empty expected value both panic at construction); `security/access_control_test.go`, `security/access_control_listener_test.go`, and `security/config/access_control_builder_test.go` extended to cover the access-control matching, `PUBLIC_ACCESS` rejection, and `AllowAnonymous` fixes above
- `validation/value_test.go`, `security/access_control_test.go` — regression coverage for the named-string-type constraint fail-open and the trailing-slash access-control bypass back-ported above
- `validation/validation_rule_internal_test.go` — regression coverage that the shorthand and parenthesized regex tag forms both accept an alternation/capture group, and that unbalanced parentheses are still rejected
- `validation/validation_rule_paren_test.go`, `validation/constraint_greater_than_nan_test.go`, `cache/in_memory_increment_ttl_test.go`, `session/copy_any_slice_test.go`, `http/result_handler_typed_nil_test.go` — regression coverage for the parenthesized-regex comma-in-group parse, the `greaterThan` `NaN` rejection, the cache-increment TTL preservation, the session `[]any` deep-copy, and the typed-nil `*Response` normalization back-ported above
- `validation/constraint_pointer_deref_test.go`, `container/container_close_value_test.go` — regression coverage for the string-constraint `*string` fail-open and the value-type service double-close back-ported above
- `container/cr38_close_order_test.go`, `http/cr38_kernel_response_test.go`, `event/cr38_adapter_race_test.go` — regression coverage for the close-order, response-replacement, and concurrent-`RegisteredEvents` fixes back-ported above
- `httpclient/transport_config.go` — `TransportConfig` (`DialTimeout`, `KeepAlive`, `MaxIdleConns`, `IdleConnTimeout`, `TlsHandshakeTimeout`, `ExpectContinueTimeout`, `ResponseHeaderTimeout`) with `DefaultTransportConfig()` exposes the previously-hardcoded `net/http.Transport` tuning of the HTTP client. Set it via the new fluent `HttpClientConfig.WithTransport(*TransportConfig)`; zero fields inherit the defaults, and a client built without it keeps the previous behaviour unchanged (backwards compatible). Back-ported from v3.
- `application/` — the HTTP graceful-shutdown grace period (previously a hardcoded `5s`) is now overridable: a `Configuration` that also implements the optional `HttpShutdownConfiguration` (`GetShutdownTimeout() time.Duration`) sets it, mirroring the existing `HttpTimeoutConfiguration` mechanism; a zero or absent value keeps the 5s default (backwards compatible). Back-ported from v3.
- `container/container_resolver_test.go`, `cache/remember_test.go` — regression coverage for the closed-container resolution guard and the cancelable-`Remember` late-joiner fix back-ported below
- `security/compiled_configuration_test.go` — regression coverage for the nil-login-result guard back-ported below
- `application/` — `Application.RegisterModuleProvider(provider)` plus expansion of the (previously dormant) `application/contract.ModuleProvider` inside `RegisterModule`: a module that also implements `ModuleProvider` now contributes its child modules in the same call, so an integration or application can register a whole group of capability-modules at once. Existing single-module registration is unchanged. Back-ported from v3.

### Fixed

- `http/kernel.go`, `http/router_utility.go`, `http/response_writer.go` — a handler that writes its own response directly to the `ResponseWriter` (a hand-rolled streaming or download handler) and then returns `(nil, nil)` no longer triggers a superfluous `WriteHeader` call. `writeResponse` synthesized a default `204 No Content` for every nil response and wrote it unconditionally, so after such a handler had already committed its status the kernel re-wrote the header — emitting a `net/http` "superfluous response.WriteHeader call" warning. The kernel now wraps the response writer in a recorder that tracks whether the headers were committed, and `writeResponse` skips writing whenever the response headers were already committed, so a streamed response is never followed by a superfluous `WriteHeader` — whether the handler returned no response or failed after committing the stream. The recorder forwards `http.Flusher`, `http.Hijacker` and `io.ReaderFrom` and exposes `Unwrap`, so streaming,
  connection-upgrade handlers (which type-assert the writer to `http.Hijacker`) and the file-serving sendfile fast path keep working through the wrapper. (Under HTTP/2 the underlying writer is not an `http.Hijacker`, so that assertion is optimistic and the `Hijack` call returns an error, handled like a missing capability; `http.Pusher` is deliberately not forwarded, as HTTP/2 server push is deprecated.) Because `net/http`'s `MaxBytesReader` detects the server response through an unexported-method assertion that does not follow `Unwrap`, the per-request body limiter is given the raw writer rather than the recorder, so an oversized request body still triggers the connection-close signal; and `Flush` records the header commit, but only when the underlying writer actually supports flushing, so a flush-only streaming handler is likewise recognised as having committed its response. The recorder also marks the response committed only when `Hijack` actually succeeds, so a handler that attempts
  a hijack which fails (and returns no response) still receives a default response rather than an empty one. When a handler commits its own response yet still returns one — or the kernel synthesizes an error response after a stream-then-panic — `writeResponse` now closes that discarded response body before skipping the write, so a `FileResponse` returned alongside a self-written stream no longer leaks its open file descriptor. Regression coverage in `http/kernel_test.go` and `http/response_writer_test.go`. Ported from the `v3` fix.
- `http/router_utility.go`, `http/response_writer.go` — `writeResponse` no longer persists the session twice when the response write fails after the headers were committed. `writeResponse` persists the session (`SaveSession`/`DeleteSession`) and then writes the response; if the write fails after the headers were committed it panics, the panic-recovery path re-enters `writeResponse`, and because `SaveSession` does not reset the session's modified flag the session store was written a second time. The recorder now tracks whether the session was already persisted for the request (`SessionPersisted`/`MarkSessionPersisted`) and `writeResponse` persists it at most once — the header-commit flag cannot gate this, as a handler that streamed its own response still needs its session persisted on that first, already-committed call. Regression coverage in `http/kernel_test.go` (`TestKernel_DoesNotDoublePersistSessionWhenWriteFailsAfterCommit`). Ported from the `v3` fix.
- `http/response.go` — `FileResponse` (and `AttachmentResponse`, which delegates to it) now resolves a served file's `Content-Type` through the same built-in fallback table the static file server uses, so a file with an extension the operating-system MIME database does not register (for example a `.ico` favicon or a web font on a minimal system such as Alpine) is served with an accurate type rather than no `Content-Type`. Previously only the static `FileServer` carried the fallback; the helper path called `mime.TypeByExtension` directly. Regression coverage in `http/response_test.go`. Ported from the `v3` fix.
- `http/static/file_server.go` — the static file server now resolves an asset's `Content-Type` through a built-in fallback table of common web types (`.ico`, `.svg`, `.css`, `.js`, web fonts, `.wasm`, …) for extensions the operating-system MIME database does not register. On a minimal system (for example Alpine) `mime.TypeByExtension(".ico")` returns empty, so a served favicon previously fell through with no `Content-Type` and defaulted to `text/plain`; it is now served as `image/x-icon`. Regression coverage in `http/static/file_server_test.go`. Ported from the `v3` fix.
- `http/kernel.go` — the per-request service-container scope is now closed even when request-logger setup fails: the `scope.Close()` defer was registered after `requestIdLogger`, so a panic during logger resolution leaked the freshly created scope on every such request. The defer is now registered immediately after `NewScope()`, with the logger reference nil-guarded for the pre-setup failure path. Ported from the `v3` fix.
- `application/application_module.go` — `RegisterModule` now guards `ModuleProvider` expansion against a provider cycle: a module that (directly or transitively) provides itself recursed without bound and overflowed the goroutine stack at boot. Expansion depth is now capped (`maxModuleProviderDepth`) and a cycle fails fast with a `module provider expansion exceeded maximum depth, possible provider cycle` panic instead of an unrecoverable stack overflow. Ported from the `v3` fix.
- `validation/validation_rule.go` — the `validate` tag grammar now accepts a regex containing a group. `parseValidationTag` classified a rule as parenthesized-form by counting `(`/`)` anywhere in the fragment, so the documented shorthand `regex=^(a|b)$` (the parens are a regex group) was misrouted to the `name(params)` branch and hard-rejected with `"invalid validation tag syntax"`, and the parenthesized `regex(pattern=^(a|b)$)` failed too — no tag spelling could express an alternation/capture group. Classification is now by position (a fragment is parenthesized only when `(` precedes any `=`), with a new `hasBalancedBrackets` helper validating the inner balance, so both spellings carry a grouped pattern verbatim. Ported from the `v3` fix.
- `validation/validation_rule.go` — the parenthesized constraint form `name(value=…)` now accepts a regex whose pattern contains a comma inside a `()` group (for example `regex(value=^(\d{1,3},){3}\d{1,3}$)`). `splitByCommaOutsideRegexMeta` (which splits a parenthesized rule's parameter list) tracked `[]`/`{}` nesting but not `()` depth, so a comma inside a regex group was treated as a parameter separator, split the value mid-pattern, and failed as `invalid validation tag syntax` — even though the shorthand `regex=…` form accepted the same pattern. The parameter splitter now tracks `()` depth too. Ported from the `v3` fix.
- `validation/constraint_greater_than.go` — `greaterThan` now rejects a floating-point `NaN` instead of silently accepting it. IEEE-754 comparisons against `NaN` are always false, so `NaN <= min` evaluated false and the value passed the bound; the constraint now rejects a non-finite float explicitly. Ported from the `v3` fix.
- `cache/in_memory.go` — `Increment`/`Decrement` no longer clear an existing key's TTL on the in-memory backend, matching the Redis backend (whose `INCRBY` preserves the key's expiry). Both paths fed `ttl=0` into the upsert, which replaced the entry with a non-expiring one, so the first increment of a key created with a TTL turned it permanent. The increment path now reuses the existing item's expiry. Ported from the `v3` fix.
- `session/file_storage.go` — `copyAnyMap` (shared by the in-memory and file session backends) now deep-copies `[]any` slices in addition to nested `map[string]any` values. Previously a slice whose elements were maps was copied by reference, so a caller mutating a map inside a slice returned by `Load` could silently corrupt the stored session data (and vice versa after `Save`). Ported from the `v3` fix.
- `http/result_handler.go` — `NormalizeResultToResponse` no longer turns a typed-nil `*Response` into a non-nil `httpcontract.Response` interface. A `ResultHandler` returning `(*Response)(nil), nil` (the idiomatic "no response" signal) passed the `*Response` type assertion as a nil pointer wrapped in a non-nil interface, so the kernel ran the writer and panicked on the nil receiver (recovered into a 500); the assertion now guards the nil pointer and returns a nil interface. Ported from the `v3` fix.
- `validation/` — the string constraints (`email`, `regex`, `alpha`, `alphanumeric`, `numeric`, `notBlank`, `min`, `max`) now dereference a pointer or interface field before inspecting it, closing a fail-open on optional `*string` fields. The validator hands each field to a constraint through `reflect.Value.Interface()` without dereferencing, so a `*string` field reached the regex-family constraints' `value.(string)` assertion as a pointer — it failed and returned `nil` (a silent PASS for any value, including an invalid email) — while `notBlank`/`min`/`max` stringified the pointer with `fmt.Sprintf("%v", value)` and validated its hexadecimal address (so `notBlank` accepted a nil pointer and `min`/`max` measured the address length). A shared `dereferenceValue` helper now unwraps pointer/interface chains (a nil pointer is treated as absent) before the existing checks, matching `greaterThan`/`lessThan`/`notEmpty`. Ported from the `v3` fix.
- `container/container_close.go` — `Close()` no longer calls `Close()` twice on a value-type (non-pointer) service registered with the default options (registered both by name and by type), and no longer panics with `hash of unhashable type` when such a service holds an unhashable interface value (a slice/map/func). Comparability is now decided from the runtime contents (`reflect.ValueOf(value).Comparable()`) rather than the static type, so an unhashable value is never used as a Go map key; and a `type:<T>` node is collapsed onto its backing `service:<name>` structurally (via `typeRegistrationNamesByType`), so even a non-comparable value-type service — which has neither a pointer nor a hashable value key — is grouped under one representative and closed exactly once. Ported from the `v3` fix.
- `container/container_close.go` — `Container.Close` could close a dependency before its dependent when the dependent resolved that dependency *by type* and the dependency was a named service that also registered its type (`WithTypeRegistration`): the same instance was tracked under both a `service:<name>` node and a `type:<T>` node, and the dependency edge constrained only one of them, so the unconstrained alias was scheduled — and closed — first. `Close` now collapses every node that resolves to the same instance onto a single representative before computing the topological close order, so a dependency edge recorded against any alias constrains the shared instance and it is closed exactly once in dependent-before-dependency order. Ported from the `v3` fix.
- `http/kernel.go` — a `kernel.response` listener that replaced the outgoing response via `KernelResponseEvent.SetResponse(...)` was silently dropped on the controller-success path and the panic-recovery path: both dispatched the event but then wrote the pre-dispatch response instead of re-reading `kernelResponseEvent.Response()`. Both sites now re-read the event response after dispatch, matching the `kernel.request` and `kernel.controller` short-circuit paths that already did so. Ported from the `v3` fix.
- `event/event_dispatcher_adapter.go` — `EventDispatcherAdapter.RegisteredEvents` sorted the map-owned listener slice in place while holding only a read lock, so two concurrent callers raced on the same backing array; it now sorts a copy. Ported from the `v3` fix.
- `validation/validation_rule.go` — a regex `validate` tag whose pattern contained a `)`, `]` or `}` **inside a character class** (for example the parenthesized `regex(value=^[)]$)`) was rejected as "invalid validation tag syntax" because `hasBalancedBrackets` counted those literals as structural delimiters. A shared `charClassScanner` now treats every member of a `[...]` class (including a literal `]` as the class's first character and a leading `^` negation) as a literal across `hasBalancedBrackets` and `splitByCommaOutsideRegexMeta`, so such patterns parse and enforce intact. Ported from the `v3` fix.
- `http/middleware/static.go` — the static file middleware merged the file server's headers onto an `EmptyResponse` (which seeds `Content-Type: text/plain`) with `Header.Add`, emitting two conflicting `Content-Type` values so a CSS/JS asset could be served as `text/plain`. The merge now `Set`s the first value of each header key (and `Add`s the rest), letting the file server's `Content-Type` replace the default. Ported from the `v3` fix.
- `validation/validation_rule.go` — a literal quote (`'` or `"`) inside a regex character class no longer mis-parses the `validate` tag. In `splitByCommaOutsideRegexMeta` the quote handlers ran independent of the character-class scanner, so a quote inside `[...]` toggled the quote state; an odd number of class-literal quotes left the flag stuck on, swallowing the top-level comma and silently dropping every following constraint (fail-open). The quote handlers are now gated on `classScanner.inClass`. Ported from the `v3` fix.
- `validation/value.go` — the string constraints (`email`, `regex`, `alpha`, `alphanumeric`, `numeric`) no longer fail open on a **defined string type** (for example `type Email string`); `dereferenceValue` now normalizes a string-kind value to a plain `string` so the `value.(string)` assertion no longer fails for a named string type and returns `nil` (a silent PASS). Ported from the `v3` fix.
- `config/environment_source.go` — the `.env` preprocessor no longer truncates an unquoted value at an inline `#` not preceded by whitespace, and the per-value `strings.TrimSpace` that defeated quoted-whitespace preservation was dropped, matching the bundled `godotenv` rule. Ported from the `v3` fix.
- `http/router_utility.go`, `http/kernel.go` — a controller that mutates or clears the session and returns a `nil` response no longer loses the session change (and the clearing `Set-Cookie`) or returns an implicit `200` instead of `204`. Ported from the `v3` fix.
- `container/container_close.go` — `Close()` is now safe against a concurrent second `Close()`: `isClosed` is set while still holding the entry lock instead of only after the close loop, so two overlapping calls no longer both snapshot and double-close every service. Ported from the `v3` fix.
- `container/scope.go` — `OverrideProtectedInstance` now checks the closed-scope flag **inside** the mutex (matching the lookup methods), closing a race where a concurrent `Close()` nilling the maps caused an `assignment to entry in nil map` panic. Ported from the `v3` fix.
- `security/compiled_configuration.go` — `CompiledFirewall.Login` no longer panics with a nil-pointer dereference when a userland `LoginHandler` returns `(nil, nil)`. The contract returns `(*LoginResult, error)`, so a handler returning neither a result nor an error is valid Go, but the firewall previously dereferenced `result.Token` unguarded inside the request goroutine; it now fails closed with a `firewall login handler returned nil result` error before the login-success event is dispatched. Ported from the `v3` fix.
- `container/container_resolver.go` — a service resolution that raced `Close()` could store its freshly created instance after the close snapshot was taken, so the instance was never closed (a connection/file-handle leak for standalone container users). The creation guard now fails fast with a `container is closed` error when the container is already closed, and a value whose creation completed while `Close()` ran is closed best-effort instead of being stored; already-created instances remain readable after `Close()`. Ported from the `v3` fix.
- `cache/remember.go` — a **cancelable** `Remember` call whose waiters all timed out cancels the leader's context, but the in-flight entry lingered until the leader's deferred cleanup ran, so a caller that joined in that window inherited the doomed call and received its cancellation error even though a fresh computation would have succeeded. A late joiner now detects the canceled call, replaces the entry, and leads a fresh computation; the leader's cleanup deletes only its own entry so it can no longer evict the replacement. Ported from the `v3` fix.

### Documentation

- `v2/README.md` — added a maintenance-mode banner clarifying that v2 receives security and critical correctness fixes only, that new features land on v3, and pointing new projects to the v3 module and its example. Aligns with the new "Versions & project status" section in the repository `README.md`.
- Comment style — `/** ... */` comments converted to `/* ... */` across the v2 `.go` files, and `// Deprecated:` markers to `/* Deprecated: ... */` (see the repository `CHANGELOG.md`). Comments-only change; no behavior change.

## [v2.7.0] - 2026-05-16 - Cron Integration, Decoupled Cron Configuration, and `.example` Flat Layout

### Added

- `cli/contract/type.go` — `StringSliceFlag` type alias for `urfavecli.StringSliceFlag`; lets commands declare repeatable string-slice flags (consumed by `integrations/cron/v2` for `--heartbeat-command` and `--heartbeat-destination`) via `clicontract.StringSliceFlag` like every other flag type
- `.documentation/package/CLI.md` — listed `clicontract.StringSliceFlag` in the package surface and added a pointer to `integrations/cron/v2/` for users looking for a crontab generator
- `v2/.example/go.mod` — `v2/.example/` is now a standalone Go module (`github.com/precision-soft/melody/v2/.example`) so it can `require` framework integrations (such as `integrations/cron/v2`) without creating a cycle with the framework's own `go.mod`; local `replace` directives keep workspace builds resolving against the in-tree melody and integrations/cron checkouts
- `v2/.example/config/` package — formerly `v2/.example/bootstrap/`, now flat-layout; each Module hook lives in its own file with a matching compile-time interface assertion at the bottom (`module.go` → `Module`, `parameter.go` → `ParameterModule`, `service.go` → `ServiceModule`, `security.go` → `SecurityModule`, `event.go` → `EventModule`, `middleware.go` → `HttpMiddlewareModule`, `http.go` → `HttpModule`, `cli.go` → `CliModule`, plus `cron.go` for the cron registry helper and `configure.go` for the entry point)
- `v2/.example/config/parameter.go` — registers cron parameters (`melody.cron.user`, `melody.cron.heartbeat_path`, `app.cron.product_user`, …) from `APP_CRON_*` env vars so the example demonstrates the env-driven cron configuration pattern
- `v2/.example/config/cron.go` — extracts the cron `Configuration` build into a dedicated helper (`newCronConfiguration(kernel)`) that reads `app.cron.product_user` from the parameter cascade and applies it as a per-command `User` on the `product:list` schedule; pedagogical demonstration of how `.env` → `RegisterParameter` → `kernel.Config().Get(...)` → `cron.EntryConfig` flow works end-to-end
- `v2/.example/config/cli.go` — `RegisterCliCommands` returns the CLI command list plus `melody:cron:generate` constructed from `newCronConfiguration(kernelInstance)`
- `v2/.example/config/service.go` — services are now registered through `(*Module).RegisterServices(kernel, registrar)` implementing `applicationcontract.ServiceModule` (instead of a top-level `registerServices(app)` function called from `Configure`)
- `v2/.example/config/middleware.go` — HTTP middleware is now registered through `(*Module).RegisterHttpMiddlewares(kernel, registrar)` implementing `applicationcontract.HttpMiddlewareModule` (instead of a direct `app.RegisterHttpMiddlewares(NewTimingMiddleware())` call from `Configure`); `NewTimingMiddleware` factory is retained
- `v2/.example/config/configure.go` — simplified to a single `app.RegisterModule(NewExampleModule())` call now that every Module* interface is implemented on `*Module` directly
- `v2/.example/security/default_access_denied_handler.go`, `v2/.example/security/login_redirect_entry_point.go` — added compile-time interface assertions (`var _ AccessDeniedHandler = ...`, `var _ EntryPoint = ...`)
- `application/application_new.go` — `computeProjectDirectory` now prefers the working directory over the closest `go.mod` ancestor when the working directory itself contains `.env` or `.env.local`. This unblocks `go run .` for sub-applications whose `.env` lives next to `main.go` rather than at the parent module's root
- `application/application_test.go` — `TestWorkingDirectoryHasEnvironmentFile_*` covers the new `.env` / `.env.local` detection helper
- `http/exception_listener_test.go`, `http/test_helper_test.go` — backfilled from v1 (introduced in v1.10.1 but never propagated) so the kernel exception listener's HTML XSS escaping, debug-mode message handling, request-id header, and existing-response preservation are now covered on v2 as well

### Changed

- `logging/default_logger.go` — rename abbreviated loop variables `i` and `v` to `index` and `value` in `joinPairs`
- `http/response.go` — rename abbreviated loop and parameter variables `r`, `b` to `runeChar`, `byteChar` in `asciiFallbackFilename`, `rfc5987EncodeFilename`, and `isRfc5987AttrChar`
- `v2/.example/` — flattened `domain/` and `infra/` layers into top-level packages (`cache/`, `cli/`, `entity/`, `event/`, `handler/`, `page/`, `presenter/`, `repository/`, `route/`, `security/`, `service/`, `subscriber/`, `url/`). Renamed `bootstrap/` to `config/`. Flat layout. Domain and in-memory repositories collapsed into a single `repository/` package
- `v2/.example/.env` — adds `APP_CRON_USER`, `APP_CRON_HEARTBEAT_PATH`, and `APP_CRON_PRODUCT_USER` so the cron default user, heartbeat path, and `product:list` per-command user are sourced from the environment rather than hard-coded
- `v2/.example/.gitignore` — ignores `/generated_conf/` (output directory for `melody:cron:generate`)
- `v2/.example/README.md` — documents the new flat layout, the cron `Configuration` registry, the env-driven cron parameters, and `melody:cron:generate` usage
- `go.work` — register the new `.example/`, `v2/.example/`, `v3/.example/` workspace modules

### Removed

- `v2/.example/bootstrap/`, `v2/.example/domain/`, `v2/.example/infra/` — flattened into top-level packages (see "Changed")

## [v2.6.0] - 2026-04-20 - Harden HTTP Server Timeouts

### Added

- `application/application_http.go` — HTTP server now sets hardened timeout defaults (`ReadTimeout=15s`, `ReadHeaderTimeout=5s`, `WriteTimeout=30s`, `IdleTimeout=60s`, `MaxHeaderBytes=1MiB`) to defend against slowloris / slow-body attacks on exposed servers (MEL-148)
- `application/application_http_timeouts.go` — new optional `HttpTimeoutConfiguration` interface; any `HttpConfiguration` that implements it can override the hardened defaults per timeout without breaking existing configurations (MEL-148)
- `application/application_http_timeouts_test.go` — coverage for default application and interface-driven overrides

## [v2.5.0] - 2026-04-17 - Extract HTTP CORS Subpackage and Harden Request Lifecycle

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

- `http/middleware.CorsConfig`, `http/middleware.NewCorsConfig`, `http/middleware.DefaultCorsConfig`, `http/middleware.RestrictiveCorsConfig`, `http/middleware.CorsMiddleware`, `http/middleware.DefaultCorsMiddleware`, `http/middleware.RestrictiveCors` — use the equivalents in `github.com/precision-soft/melody/v2/http/cors` instead. Deprecated symbols are kept for backwards compatibility; no removal scheduled.

## [v2.4.1] - 2026-04-17 - Fix Compression Error Propagation and Concurrent Access Races

### Fixed

- `http/middleware/compression.go` — compression middleware now propagates `io.ReadAll` errors instead of silently returning partial data to the client
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

## [v2.4.0] - 2026-04-14 - Improve Goroutine Lifecycle and Default Logger

### Changed

- `cache/in_memory.go` — `cleanupLoop` accepts `context.Context`; `NewInMemoryCache` creates a cancel context stored as `cleanupCancel`; `Close()` calls `cleanupCancel()` to stop the goroutine cooperatively
- `session/in_memory_storage.go` — same goroutine lifecycle improvements as `cache/in_memory.go`
- `http/request.go` — replace `log.Printf` fallback (when no runtime instance is available) with `logging.NewDefaultLogger().Warning(...)`; remove unused `"log"` import
- `cli/command.go` — remove block comments and `//nolint:errcheck` directives from `printGreenFullLine`, `printGreenStatusLine`, `printRedStatusLine` closures
- `logging/logger.go` — add GoDoc comment to `causeChainMaxDepth` constant
- `security/compiled_configuration.go` — group string fields in `CompiledFirewall` struct (`name`, `matcherDescription`, `loginPath`, `logoutPath`)

## [v2.3.0] - 2026-04-13 - Fix Validators, Rate Limiter, and Router

### Fixed

- `validator.go` — `createConstraintWithParams` now handles `greaterThan` parameters; `validate:"greaterThan(value=5)"` was silently using `min=0`
- `rate_limit.go` — `getClientIp` strips port via `net.SplitHostPort`; rate limiting was per-connection instead of per-IP
- `url_generator.go` — path parameters now URL-encoded via `url.PathEscape`; special characters produced malformed URLs
- `accept.go` — `PrefersHtml` uses position-based comparison; browsers sending both `text/html` and `application/json` now correctly get HTML
- `compression.go` — `gzip.NoCompression` (level 0) is no longer overridden to default compression
- `constraint_greater_than.go` — added `float32`/`float64` support; float values no longer return "value must be an integer"
- `kernel.go` — `errorHandler` now called for controller errors (was only called on panic recovery path)
- `cors.go` — panic at middleware initialization when `AllowCredentials=true` and origins contain `"*"` to prevent overly permissive CORS

### Changed

- `file_storage.go` — `copyAnyMap` performs recursive deep copy for nested `map[string]any` values
- `exception/utility.go` — export `BuildCauseChain` and `BuildCauseContextChain`
- `logging/logger.go` — remove duplicated cause chain functions; delegate to `exception.BuildCauseChain` / `exception.BuildCauseContextChain`
- `router_utility.go` — remove implicit HEAD-to-GET match from `matchesMethod`; kernel `HeadFallbackToGet` policy is now the single control point
- `httpclient/http_client.go` — extract shared request-building logic into `buildRequest` helper; `Request` and `RequestStream` both delegate to it

## [v2.2.4] - 2026-04-10 - Fix XSS, Symlink Traversal, and Routing Edge Cases

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

## [v2.2.3] - 2026-03-21 - Refactor Address Colon Check in Config

### Changed

- `config/http.go` — replaced colon-based address check with `strings.Contains` for correct host:port detection

## [v2.2.2] - 2026-03-18 - Fix HTTP HEAD Handling and Update Dev Scripts

### Fixed

- `http/router_utility.go` — aligned HEAD handling and response contract validation; prevents incorrect responses on HEAD requests

### Changed

- `internal/reflect.go` — updated type-reflection utilities

## [v2.2.1] - 2026-03-17 - Fix JSON Logging Level Label Preservation

### Fixed

- `logging/contract/level.go`, `logging/logger.go` — preserved numeric logging level labels in JSON output; `logging/json_logger_test.go` — coverage

## [v2.2.0] - 2026-03-17 - Add Module Configuration Registration and Logging Labels

### Added

- `application/contract/config_module.go` — new `ConfigModule` interface allowing modules to register configuration during application boot
- `logging/contract/config.go`, `logging/logging_config.go` — `LoggingConfig` struct and contract for customizable logging level labels
- `logging/default_logger.go`, `logging/json_logger.go`, `logging/logger.go` — updated to apply level label customization from `LoggingConfig`
- `application/application.go`, `application/application_module.go`, `application/application_new.go` — wired `ConfigModule` into the application boot sequence

## [v2.1.3] - 2026-03-05 - Add CLI Table Width Flag for Table Output

### Added

- `cli/output/flag.go`, `cli/output/printer_selector.go` — added `--table-width` flag for table output
- `cli/output/option.go`, `cli/output/option_parser.go`, `cli/output/standard_flag.go` — parsed and propagated new width option

## [v2.1.2] - 2026-02-28 - Add CLI Stdout/Stderr Wiring and Standardize Method Receivers

### Changed

- All `*.go` files in the module — standardized all method receivers to `instance` for consistent style

### Added

- `cli/command.go`, `cli/command_output.go` — wired `stdout`/`stderr` to CLI output; print command errors with failed exit status

## [v2.1.1] - 2026-02-23 - Fix RoleVoter Auto-Upgrade to RoleHierarchyVoter

### Fixed

- `security/config/compile.go`, `security/access_decision_manager.go` — auto-upgrade `RoleVoter` to `RoleHierarchyVoter` when role hierarchy is configured

## [v2.1.0] - 2026-02-18 - Add GreaterThan and NotEmpty Validation Constraints

### Fixed

- `CONTRIBUTING.md` — broken documentation links corrected

### Added

- `validation/constraint_greater_than.go` — new `greaterThan(value=N)` constraint with support for int, float32, float64; returns per-constraint error codes
- `validation/constraint_not_empty.go` — new `notEmpty` constraint for slices and strings; returns per-constraint error codes
- `validation/const.go`, `validation/validation_rule.go`, `validation/validator.go` — wired new constraints into the validation pipeline
- `exception/utility.go` — context-aware error wrapping helper `Wrap(ctx, err)` for exception chaining

## [v2.0.0] - 2026-02-17 - Introduce Melody v2 Module

### Added

- `go.mod` — introduce Melody v2 module (`github.com/precision-soft/melody/v2`)

[Unreleased]: https://github.com/precision-soft/melody/compare/v2.8.1...HEAD

[v2.8.1]: https://github.com/precision-soft/melody/compare/v2.8.0...v2.8.1

[v2.8.0]: https://github.com/precision-soft/melody/compare/v2.7.0...v2.8.0

[v2.7.0]: https://github.com/precision-soft/melody/compare/v2.6.0...v2.7.0

[v2.6.0]: https://github.com/precision-soft/melody/compare/v2.5.0...v2.6.0

[v2.5.0]: https://github.com/precision-soft/melody/compare/v2.4.1...v2.5.0

[v2.4.1]: https://github.com/precision-soft/melody/compare/v2.4.0...v2.4.1

[v2.4.0]: https://github.com/precision-soft/melody/compare/v2.3.0...v2.4.0

[v2.3.0]: https://github.com/precision-soft/melody/compare/v2.2.4...v2.3.0

[v2.2.4]: https://github.com/precision-soft/melody/compare/v2.2.3...v2.2.4

[v2.2.3]: https://github.com/precision-soft/melody/compare/v2.2.2...v2.2.3

[v2.2.2]: https://github.com/precision-soft/melody/compare/v2.2.1...v2.2.2

[v2.2.1]: https://github.com/precision-soft/melody/compare/v2.2.0...v2.2.1

[v2.2.0]: https://github.com/precision-soft/melody/compare/v2.1.3...v2.2.0

[v2.1.3]: https://github.com/precision-soft/melody/compare/v2.1.2...v2.1.3

[v2.1.2]: https://github.com/precision-soft/melody/compare/v2.1.1...v2.1.2

[v2.1.1]: https://github.com/precision-soft/melody/compare/v2.1.0...v2.1.1

[v2.1.0]: https://github.com/precision-soft/melody/compare/v2.0.0...v2.1.0

[v2.0.0]: https://github.com/precision-soft/melody/compare/v1.6.3...v2.0.0
