# Changelog

All notable changes to `precision-soft/melody` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v1.14.0] - 2026-06-16 - Configurable Transport & Shutdown Tunables + v3 Security and Correctness Back-ports

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
- `httpclient/transport_config.go` — `TransportConfig` (`DialTimeout`, `KeepAlive`, `MaxIdleConns`, `IdleConnTimeout`, `TlsHandshakeTimeout`, `ExpectContinueTimeout`, `ResponseHeaderTimeout`) with `DefaultTransportConfig()` exposes the previously-hardcoded `net/http.Transport` tuning of the HTTP client. Set it via the new fluent `HttpClientConfig.WithTransport(*TransportConfig)`; zero fields inherit the defaults, and a client built without it keeps the previous behaviour unchanged (backwards compatible). Back-ported from v3.
- `application/` — the HTTP graceful-shutdown grace period (previously a hardcoded `5s`) is now overridable: a `Configuration` that also implements the optional `HttpShutdownConfiguration` (`GetShutdownTimeout() time.Duration`) sets it, mirroring the existing `HttpTimeoutConfiguration` mechanism; a zero or absent value keeps the 5s default (backwards compatible). Back-ported from v3.
- `container/container_resolver_test.go`, `cache/remember_test.go` — regression coverage for the closed-container resolution guard and the cancelable-`Remember` late-joiner fix back-ported below
- `security/compiled_configuration_test.go` — regression coverage for the nil-login-result guard back-ported below
- `application/` — `Application.RegisterModuleProvider(provider)` plus expansion of the (previously dormant) `application/contract.ModuleProvider` inside `RegisterModule`: a module that also implements `ModuleProvider` now contributes its child modules in the same call, so an integration or application can register a whole group of capability-modules at once. Existing single-module registration is unchanged. Back-ported from v3.

### Changed

- `.dev/docker/docker-compose.yml`, `.dev/docker/.env`, `dc` — the development recipe now starts in two categories: `./dc up:minimal` starts only the `dev` container (enough for the build-tag matrix and unit tests), and `./dc up:all` also starts the infrastructure services needed by the live end-to-end tests (`rabbitmq`, `redis`, `mysql`, `minio`, grouped under the compose profile `all`); `./dc down` tears down both categories. Every published host port is now an `.env` variable (`DEV_HTTP_HOST_PORT`, `RABBITMQ_HOST_PORT`, `RABBITMQ_MANAGEMENT_HOST_PORT`, `REDIS_HOST_PORT`, `MYSQL_HOST_PORT`, `MINIO_HOST_PORT`, `MINIO_CONSOLE_HOST_PORT`) with the previous values as defaults, so a machine where another stack already holds a port can override it in `.dev/docker/.env.local`
- `.dev/docker/.gitignore` — `.env.local` is no longer tracked (it is machine-local by design and auto-created by the `dc` wrapper); it is now ignored alongside `.bash_aliases_local`
- `.dev/docker/Dockerfile`, `.dev/docker/entrypoint.sh`, `.dev/docker/docker-compose.yml` — the `dev` container now boots the `v3/.example` application by default with `reflex` hot-reload (rebuild-and-restart on `.go`/`.env`/`.yaml`/`.json`/`.toml` changes), so `./dc up:minimal` brings up a live example on `DEV_HTTP_HOST_PORT` (default `8180`). `github.com/cespare/reflex` is installed in the image. Three environment knobs override the behaviour (defaulted in compose, settable in `.dev/docker/.env.local`): `MELODY_DEV_REFLEX_ENABLED` (`0` runs once without watching), `MELODY_DEV_EXAMPLE_DIR` (point at `./.example` or `v2/.example`), and `MELODY_DEV_RUN_COMMAND` (empty idles the container like before). The example boots in-memory by default and wires the live services when their env vars are set under `./dc up:all`
- `.dev/docker/load-balancer/vhost.conf`, `.dev/docker/docker-compose.yml`, `.dev/docker/.env`, `dc` — a new `load-balancer` service (nginx) reverse-proxies the example over plain HTTP at `http://example.melody.localhost.precision-soft.com` (the `*.localhost.precision-soft.com` wildcard resolves to `127.0.0.1`), so there are no localhost-certificate issues. It starts alongside the example under both `./dc up:minimal` and `./dc up:all`, resolves the `dev` upstream through the docker DNS resolver at request time (so it comes up even before the app), and forwards WebSocket upgrades for the example's `/ws` route. The published host port is the new `LOAD_BALANCER_HTTP_HOST_PORT` `.env` variable (default `80`)

### Fixed

- `http/kernel.go`, `http/router_utility.go`, `http/response_writer.go` — a handler that writes its own response directly to the `ResponseWriter` (a hand-rolled streaming or download handler) and then returns `(nil, nil)` no longer triggers a superfluous `WriteHeader` call. `writeResponse` synthesized a default `204 No Content` for every nil response and wrote it unconditionally, so after such a handler had already committed its status the kernel re-wrote the header — emitting a `net/http` "superfluous response.WriteHeader call" warning. The kernel now wraps the response writer in a recorder that tracks whether the headers were committed, and `writeResponse` skips writing whenever the response headers were already committed, so a streamed response is never followed by a superfluous `WriteHeader` — whether the handler returned no response or failed after committing the stream. The recorder forwards `http.Flusher`, `http.Hijacker` and `io.ReaderFrom` and exposes `Unwrap`, so streaming, connection-upgrade handlers (which type-assert the writer to `http.Hijacker`) and the file-serving sendfile fast path keep working through the wrapper. (Under HTTP/2 the underlying writer is not an `http.Hijacker`, so that assertion is optimistic and the `Hijack` call returns an error, handled like a missing capability; `http.Pusher` is deliberately not forwarded, as HTTP/2 server push is deprecated.) Because `net/http`'s `MaxBytesReader` detects the server response through an unexported-method assertion that does not follow `Unwrap`, the per-request body limiter is given the raw writer rather than the recorder, so an oversized request body still triggers the connection-close signal; and `Flush` records the header commit, but only when the underlying writer actually supports flushing, so a flush-only streaming handler is likewise recognised as having committed its response. The recorder also marks the response committed only when `Hijack` actually succeeds, so a handler that attempts a hijack which fails (and returns no response) still receives a default response rather than an empty one. When a handler commits its own response yet still returns one — or the kernel synthesizes an error response after a stream-then-panic — `writeResponse` now closes that discarded response body before skipping the write, so a `FileResponse` returned alongside a self-written stream no longer leaks its open file descriptor. Regression coverage in `http/kernel_test.go` and `http/response_writer_test.go`. Ported from the `v3` fix.
- `http/router_utility.go`, `http/response_writer.go` — `writeResponse` no longer persists the session twice when the response write fails after the headers were committed. `writeResponse` persists the session (`SaveSession`/`DeleteSession`) and then writes the response; if the write fails after the headers were committed it panics, the panic-recovery path re-enters `writeResponse`, and because `SaveSession` does not reset the session's modified flag the session store was written a second time. The recorder now tracks whether the session was already persisted for the request (`SessionPersisted`/`MarkSessionPersisted`) and `writeResponse` persists it at most once — the header-commit flag cannot gate this, as a handler that streamed its own response still needs its session persisted on that first, already-committed call. Regression coverage in `http/kernel_test.go` (`TestKernel_DoesNotDoublePersistSessionWhenWriteFailsAfterCommit`). Ported from the `v3` fix.
- `http/response.go` — `FileResponse` (and `AttachmentResponse`, which delegates to it) now resolves a served file's `Content-Type` through the same built-in fallback table the static file server uses, so a file with an extension the operating-system MIME database does not register (for example a `.ico` favicon or a web font on a minimal system such as Alpine) is served with an accurate type rather than no `Content-Type`. Previously only the static `FileServer` carried the fallback; the helper path called `mime.TypeByExtension` directly. Regression coverage in `http/response_test.go`. Ported from the `v3` fix.
- `http/static/file_server.go` — the static file server now resolves an asset's `Content-Type` through a built-in fallback table of common web types (`.ico`, `.svg`, `.css`, `.js`, web fonts, `.wasm`, …) for extensions the operating-system MIME database does not register. On a minimal system (for example Alpine) `mime.TypeByExtension(".ico")` returns empty, so a served favicon previously fell through with no `Content-Type` and defaulted to `text/plain`; it is now served as `image/x-icon`. Regression coverage in `http/static/file_server_test.go`. Ported from the `v3` fix.
- `http/kernel.go` — the per-request service-container scope is now closed even when request-logger setup fails: the `scope.Close()` defer was registered after `requestIdLogger`, so a panic during logger resolution leaked the freshly created scope on every such request. The defer is now registered immediately after `NewScope()`, with the logger reference nil-guarded for the pre-setup failure path. Ported from the `v3` fix.
- `http/kernel.go` — a `kernel.response` (`EventKernelResponse`) listener that replaced the response via `SetResponse` was silently ignored on the two primary paths: the controller success path and the panic-recovery path dispatched the event but never read the (possibly replaced) response back from it, so `writeResponse` always wrote the pre-listener response. Both paths now capture `kernelResponseEvent.Response()` after dispatch, matching the kernel-request and kernel-controller short-circuit paths — and `v2`/`v3`, which were already correct. Found by back-filling the v1 kernel test suite to parity with `v2`/`v3`; regression coverage in `http/kernel_test.go` (`TestKernel_ResponseListenerReplacesResponseOnSuccessPath`, `TestKernel_ResponseListenerReplacesResponseOnPanicRecoveryPath`).
- `application/application_module.go` — `RegisterModule` now guards `ModuleProvider` expansion against a provider cycle: a module that (directly or transitively) provides itself recursed without bound and overflowed the goroutine stack at boot. Expansion depth is now capped (`maxModuleProviderDepth`) and a cycle fails fast with a `module provider expansion exceeded maximum depth, possible provider cycle` panic instead of an unrecoverable stack overflow. Ported from the `v3` fix.
- `validation/validation_rule.go` — the `validate` tag grammar now accepts a regex containing a group. `parseValidationTag` classified a rule as parenthesized-form by counting `(`/`)` anywhere in the fragment, so the documented shorthand `regex=^(a|b)$` (the parens are a regex group) was misrouted to the `name(params)` branch and hard-rejected with `"invalid validation tag syntax"`, and the parenthesized `regex(pattern=^(a|b)$)` failed too — no tag spelling could express an alternation/capture group. Classification is now by position (a fragment is parenthesized only when `(` precedes any `=`), with a new `hasBalancedBrackets` helper validating the inner balance, so both spellings carry a grouped pattern verbatim. Ported from the `v3` fix.
- `validation/validation_rule.go` — the parenthesized constraint form `name(value=…)` now accepts a regex whose pattern contains a comma inside a `()` group (for example `regex(value=^(\d{1,3},){3}\d{1,3}$)`). `splitByCommaOutsideRegexMeta` (which splits a parenthesized rule's parameter list) tracked `[]`/`{}` nesting but not `()` depth, so a comma inside a regex group was treated as a parameter separator, split the value mid-pattern, and failed as `invalid validation tag syntax` — even though the shorthand `regex=…` form accepted the same pattern. The parameter splitter now tracks `()` depth too. Ported from the `v3` fix.
- `validation/constraint_greater_than.go` — `greaterThan` now rejects a floating-point `NaN` instead of silently accepting it. IEEE-754 comparisons against `NaN` are always false, so `NaN <= min` evaluated false and the value passed the bound; the constraint now rejects a non-finite float explicitly. Ported from the `v3` fix.
- `cache/in_memory.go` — `Increment`/`Decrement` no longer clear an existing key's TTL on the in-memory backend, matching the Redis backend (whose `INCRBY` preserves the key's expiry). Both paths fed `ttl=0` into the upsert, which replaced the entry with a non-expiring one, so the first increment of a key created with a TTL turned it permanent. The increment path now reuses the existing item's expiry. Ported from the `v3` fix.
- `session/file_storage.go` — `copyAnyMap` (shared by the in-memory and file session backends) now deep-copies `[]any` slices in addition to nested `map[string]any` values. Previously a slice whose elements were maps was copied by reference, so a caller mutating a map inside a slice returned by `Load` could silently corrupt the stored session data (and vice versa after `Save`). Ported from the `v3` fix.
- `http/result_handler.go` — `NormalizeResultToResponse` no longer turns a typed-nil `*Response` into a non-nil `httpcontract.Response` interface. A `ResultHandler` returning `(*Response)(nil), nil` (the idiomatic "no response" signal) passed the `*Response` type assertion as a nil pointer wrapped in a non-nil interface, so the kernel ran the writer and panicked on the nil receiver (recovered into a 500); the assertion now guards the nil pointer and returns a nil interface. Ported from the `v3` fix.
- `validation/` — the string constraints (`email`, `regex`, `alpha`, `alphanumeric`, `numeric`, `notBlank`, `min`, `max`) now dereference a pointer or interface field before inspecting it, closing a fail-open on optional `*string` fields. The validator hands each field to a constraint through `reflect.Value.Interface()` without dereferencing, so a `*string` field reached the regex-family constraints' `value.(string)` assertion as a pointer — it failed and returned `nil` (a silent PASS for any value, including an invalid email) — while `notBlank`/`min`/`max` stringified the pointer with `fmt.Sprintf("%v", value)` and validated its hexadecimal address (so `notBlank` accepted a nil pointer and `min`/`max` measured the address length). A shared `dereferenceValue` helper now unwraps pointer/interface chains (a nil pointer is treated as absent) before the existing checks, matching `greaterThan`/`lessThan`/`notEmpty`. Ported from the `v3` fix.
- `container/container_close.go` — `Close()` no longer calls `Close()` twice on a value-type (non-pointer) service registered with the default options (registered both by name and by type), and no longer panics with `hash of unhashable type` when such a service holds an unhashable interface value (a slice/map/func). Duplicate suppression was keyed by pointer identity only, so the two close candidates referring to the same value-receiver service were both closed; a comparable value is now deduplicated by value identity, and comparability is decided from the runtime contents (`reflect.ValueOf(value).Comparable()`) rather than the static type, so an unhashable value is routed to the non-deduplicated path instead of panicking when used as a Go map key. Ported from the `v3` fix.
- `validation/validation_rule.go` — a regex `validate` tag whose pattern contained a `)`, `]` or `}` **inside a character class** (for example the parenthesized `regex(value=^[)]$)`) was rejected as "invalid validation tag syntax" because `hasBalancedBrackets` counted those literals as structural delimiters. A shared `charClassScanner` now treats every member of a `[...]` class (including a literal `]` as the class's first character and a leading `^` negation) as a literal across `hasBalancedBrackets` and `splitByCommaOutsideRegexMeta`, so such patterns parse and enforce intact. Ported from the `v3` fix.
- `http/middleware/static.go` — the static file middleware merged the file server's headers onto an `EmptyResponse` (which seeds `Content-Type: text/plain`) with `Header.Add`, emitting two conflicting `Content-Type` values so a CSS/JS asset could be served as `text/plain`. The merge now `Set`s the first value of each header key (and `Add`s the rest), letting the file server's `Content-Type` replace the default. Ported from the `v3` fix.
- `validation/validation_rule.go` — a literal quote (`'` or `"`) inside a regex character class no longer mis-parses the `validate` tag. In `splitByCommaOutsideRegexMeta` the quote handlers ran independent of the character-class scanner, so a quote inside `[...]` toggled the quote state; an odd number of class-literal quotes left the flag stuck on, swallowing the top-level comma and silently dropping every following constraint (fail-open). The quote handlers are now gated on `classScanner.inClass`. Ported from the `v3` fix.
- `validation/value.go` — the string constraints (`email`, `regex`, `alpha`, `alphanumeric`, `numeric`) no longer fail open on a **defined string type** (for example `type Email string`). `dereferenceValue` returned the value with its dynamic type, so the constraints' `value.(string)` assertion failed for a named string type and returned `nil` — a silent PASS for any value, the same fail-open the `*string` fix closed for pointers but reached through a domain-typed request field. `dereferenceValue` now normalizes a string-kind value to a plain `string`. Ported from the `v3` fix.
- `config/environment_source.go` — the `.env` preprocessor no longer truncates an unquoted value at an inline `#` not preceded by whitespace (`COLOR=#ffffff` became empty, `PASSWORD=ab#cd` truncated to `ab`), matching the bundled `godotenv` rule, and the per-value `strings.TrimSpace` that defeated quoted-whitespace preservation was dropped. Ported from the `v3` fix.
- `http/router_utility.go`, `http/kernel.go` — a controller that mutates or clears the session and returns a `nil` response no longer loses the session change (and the clearing `Set-Cookie`) or returns an implicit `200` instead of `204`. Session persistence lived only in `writeResponse`'s non-nil branch and the kernel skipped `writeResponse` entirely on the `(nil, nil)` path; the kernel now calls it and `writeResponse` synthesizes an empty `204`. Ported from the `v3` fix.
- `container/container_close.go` — `Close()` is now safe against a concurrent second `Close()`: `isClosed` is set while still holding the entry lock instead of only after the close loop, so two overlapping calls no longer both snapshot and double-close every service. Ported from the `v3` fix.
- `container/scope.go` — `OverrideProtectedInstance` now checks the closed-scope flag **inside** the mutex (matching the lookup methods), closing a race where a concurrent `Close()` nilling the maps caused an `assignment to entry in nil map` panic. Ported from the `v3` fix.
- `security/compiled_configuration.go` — `CompiledFirewall.Login` no longer panics with a nil-pointer dereference when a userland `LoginHandler` returns `(nil, nil)`. The contract returns `(*LoginResult, error)`, so a handler returning neither a result nor an error is valid Go, but the firewall previously dereferenced `result.Token` unguarded inside the request goroutine; it now fails closed with a `firewall login handler returned nil result` error before the login-success event is dispatched. Ported from the `v3` fix.
- `container/container_resolver.go` — a service resolution that raced `Close()` could store its freshly created instance after the close snapshot was taken, so the instance was never closed (a connection/file-handle leak for standalone container users). The creation guard now fails fast with a `container is closed` error when the container is already closed, and a value whose creation completed while `Close()` ran is closed best-effort instead of being stored; already-created instances remain readable after `Close()`. Ported from the `v3` fix.
- `cache/remember.go` — a **cancelable** `Remember` call whose waiters all timed out cancels the leader's context, but the in-flight entry lingered until the leader's deferred cleanup ran, so a caller that joined in that window inherited the doomed call and received its cancellation error even though a fresh computation would have succeeded. A late joiner now detects the canceled call, replaces the entry, and leads a fresh computation; the leader's cleanup deletes only its own entry so it can no longer evict the replacement. Ported from the `v3` fix.

### Documentation

- `README.md` — added a "Getting started" section (install, a minimal runnable HTTP application, and next steps) and a "Versions & project status" section: the v1/v2/v3 module lines, v3 being the actively maintained version, the security/critical-fix back-port policy, the deprecate-toward-v4 approach, and the rationale for the intentional version duplication. Added an "Integrations" pointer and moved the build-tag reference below the usage guidance.
- `CONTRIBUTING.md` — added a "Versioning and where to make changes" section (features land on v3 only; back-port to v1/v2 only for security or critical correctness fixes; the version duplication is intentional and must not be consolidated), documented the `./dc up:minimal` / `up:all` development shell, and pointed the security guidance at `SECURITY.md`.
- `SECURITY.md` — added: supported version lines and private vulnerability reporting through GitHub.
- `integrations/README.md` — added an integrations index (what each integration provides, supported version lines, and links to per-integration documentation).
- `CODE_OF_CONDUCT.md` — added (Contributor Covenant 2.1 by reference; private reporting through GitHub).
- `.github/` — added issue templates (bug, feature), an issue-template config that links private security reporting and disables blank issues, and a pull-request template that reflects the versioning and back-port rules.
- Comment style — the house comment delimiter changed from `/** ... */` to `/* ... */` across all `.go` files. Single-star block comments render correctly on `pkg.go.dev` and machine-recognize the `Deprecated:` marker, so the previous `// Deprecated:` exception was dropped and existing markers were converted to `/* Deprecated: ... */`. `CONTRIBUTING.md` and the documentation canon were updated accordingly. Comments-only change; no behavior change.

## [v1.13.0] - 2026-05-16 - Cron Integration, Decoupled Cron Configuration, and `.example` Flat Layout

### Added

- `cli/contract/type.go` — `StringSliceFlag` type alias for `urfavecli.StringSliceFlag`; lets commands declare repeatable string-slice flags (consumed by `integrations/cron` for `--heartbeat-command` and `--heartbeat-destination`) via `clicontract.StringSliceFlag` like every other flag type
- `.documentation/package/CLI.md` — listed `clicontract.StringSliceFlag` in the package surface and added a pointer to `integrations/cron/` for users looking for a crontab generator
- `.example/go.mod` — `.example/` is now a standalone Go module (`github.com/precision-soft/melody/.example`) so it can `require` framework integrations (such as `integrations/cron`) without creating a cycle with the framework's own `go.mod`; local `replace` directives keep workspace builds resolving against the in-tree melody and integrations/cron checkouts
- `.example/config/` package — formerly `.example/bootstrap/`, now flat-layout; each Module hook lives in its own file with a matching compile-time interface assertion at the bottom (`module.go` → `Module`, `parameter.go` → `ParameterModule`, `service.go` → `ServiceModule`, `security.go` → `SecurityModule`, `event.go` → `EventModule`, `middleware.go` → `HttpMiddlewareModule`, `http.go` → `HttpModule`, `cli.go` → `CliModule`, plus `cron.go` for the cron registry helper and `configure.go` for the entry point)
- `.example/config/parameter.go` — registers cron parameters (`melody.cron.user`, `melody.cron.heartbeat_path`, `app.cron.product_user`, …) from `APP_CRON_*` env vars so the example demonstrates the env-driven cron configuration pattern
- `.example/config/cron.go` — extracts the cron `Configuration` build into a dedicated helper (`newCronConfiguration(kernel)`) that reads `app.cron.product_user` from the parameter cascade and applies it as a per-command `User` on the `product:list` schedule; pedagogical demonstration of how `.env` → `RegisterParameter` → `kernel.Config().Get(...)` → `cron.EntryConfig` flow works end-to-end
- `.example/config/cli.go` — `RegisterCliCommands` returns the CLI command list plus `melody:cron:generate` constructed from `newCronConfiguration(kernelInstance)`
- `.example/config/service.go` — services are now registered through `(*Module).RegisterServices(kernel, registrar)` implementing `applicationcontract.ServiceModule` (instead of a top-level `registerServices(app)` function called from `Configure`)
- `.example/config/middleware.go` — HTTP middleware is now registered through `(*Module).RegisterHttpMiddlewares(kernel, registrar)` implementing `applicationcontract.HttpMiddlewareModule` (instead of a direct `app.RegisterHttpMiddlewares(NewTimingMiddleware())` call from `Configure`); `NewTimingMiddleware` factory is retained
- `.example/config/configure.go` — simplified to a single `app.RegisterModule(NewExampleModule())` call now that every Module* interface is implemented on `*Module` directly
- `.example/security/default_access_denied_handler.go`, `.example/security/login_redirect_entry_point.go` — added compile-time interface assertions (`var _ AccessDeniedHandler = ...`, `var _ EntryPoint = ...`)
- `application/application_new.go` — `computeProjectDirectory` now prefers the working directory over the closest `go.mod` ancestor when the working directory itself contains `.env` or `.env.local`. This unblocks `go run .` for sub-applications whose `.env` lives next to `main.go` rather than at the parent module's root
- `application/application_test.go` — `TestWorkingDirectoryHasEnvironmentFile_*` covers the new `.env` / `.env.local` detection helper

### Changed

- `http/accept.go` — `PrefersHtml` refactored to short-circuit when `text/html` is absent from the `Accept` header, skipping the `application/json` scan and reducing the common-case complexity from O(2N) to O(N); v1/v2/v3 implementations are now byte-identical apart from the melody import path
- `logging/default_logger.go` — rename abbreviated loop variables `i` and `v` to `index` and `value` in `joinPairs`
- `http/response.go` — rename abbreviated loop and parameter variables `r`, `b` to `runeChar`, `byteChar` in `asciiFallbackFilename`, `rfc5987EncodeFilename`, and `isRfc5987AttrChar`
- `.example/` — flattened `domain/` and `infra/` layers into top-level packages (`cache/`, `cli/`, `entity/`, `event/`, `handler/`, `page/`, `presenter/`, `repository/`, `route/`, `security/`, `service/`, `subscriber/`, `url/`). Renamed `bootstrap/` to `config/`. Flat layout. Domain and in-memory repositories collapsed into a single `repository/` package
- `.example/.env` — adds `APP_CRON_USER`, `APP_CRON_HEARTBEAT_PATH`, and `APP_CRON_PRODUCT_USER` so the cron default user, heartbeat path, and `product:list` per-command user are sourced from the environment rather than hard-coded
- `.example/.gitignore` — ignores `/generated_conf/` (output directory for `melody:cron:generate`)
- `.example/README.md` — documents the new flat layout, the cron `Configuration` registry, the env-driven cron parameters, and `melody:cron:generate` usage
- `go.work` — register the new `.example/`, `v2/.example/`, `v3/.example/` workspace modules

### Removed

- `.example/bootstrap/`, `.example/domain/`, `.example/infra/` — flattened into top-level packages (see "Changed")

## [v1.12.1] - 2026-04-23 - Retract v1.10.0

### Changed

- `go.mod` — retract `v1.10.0`; the tag was placed on the wrong commit (identical to `v1.9.0`); use `v1.10.1` instead

## [v1.12.0] - 2026-04-20 - Harden HTTP Server Timeouts

### Added

- `application/application_http.go` — HTTP server now sets hardened timeout defaults (`ReadTimeout=15s`, `ReadHeaderTimeout=5s`, `WriteTimeout=30s`, `IdleTimeout=60s`, `MaxHeaderBytes=1MiB`) to defend against slowloris / slow-body attacks on exposed servers (MEL-148)
- `application/application_http_timeouts.go` — new optional `HttpTimeoutConfiguration` interface; any `HttpConfiguration` that implements it can override the hardened defaults per timeout without breaking existing configurations (MEL-148)
- `application/application_http_timeouts_test.go` — coverage for default application and interface-driven overrides

## [v1.11.0] - 2026-04-17 - Extract HTTP CORS Subpackage and Harden Request Lifecycle

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

- `http/middleware.CorsConfig`, `http/middleware.NewCorsConfig`, `http/middleware.DefaultCorsConfig`, `http/middleware.RestrictiveCorsConfig`, `http/middleware.CorsMiddleware`, `http/middleware.DefaultCorsMiddleware`, `http/middleware.RestrictiveCors` — use the equivalents in `github.com/precision-soft/melody/http/cors` instead. Deprecated symbols are kept for backwards compatibility; no removal scheduled.

## [v1.10.1] - 2026-04-17 - Fix Compression Error Propagation and Concurrent Access Races

### Fixed

- `http/middleware/compression.go` — compression middleware now propagates `io.ReadAll` errors instead of silently returning partial data to the client; level validation lower bound corrected from `gzip.DefaultCompression` to `gzip.HuffmanOnly`
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

## [v1.10.0] - 2026-04-13 - Lock-Step Release — Align with v2/v3 Sibling Tags

Lock-step release — no `v1/` changes this cycle. Tag SHA equals `v1.9.0`; published to keep the core `v1` module version aligned with the `v2.4.0` / `v3.3.0` sibling tags. See the v2/v3 CHANGELOGs for the actual content of this cycle.

## [v1.9.0] - 2026-04-13 - Fix Validators, Rate Limiter, and Router; Improve Goroutine Lifecycle

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

- `cache/in_memory.go` — `cleanupLoop` accepts `context.Context`; `NewInMemoryCache` creates a cancel context stored as `cleanupCancel`; `Close()` calls `cleanupCancel()` to stop the goroutine cooperatively
- `session/in_memory_storage.go` — same goroutine lifecycle improvements as `cache/in_memory.go`
- `http/request.go` — replace `log.Printf` fallback (when no runtime instance is available) with `logging.NewDefaultLogger().Warning(...)`; remove unused `"log"` import
- `cli/command.go` — remove block comments and `//nolint:errcheck` directives from `printGreenFullLine`, `printGreenStatusLine`, `printRedStatusLine` closures
- `logging/logger.go` — added GoDoc comment to `causeChainMaxDepth`; removed duplicated `buildCauseChain`/`buildCauseContextChain`, delegating to `exception.BuildCauseChain`/`BuildCauseContextChain`
- `security/compiled_configuration.go` — group string fields in `CompiledFirewall` struct (`name`, `matcherDescription`, `loginPath`, `logoutPath`)
- `file_storage.go` — `copyAnyMap` performs recursive deep copy for nested `map[string]any` values
- `exception/utility.go` — export `BuildCauseChain` and `BuildCauseContextChain` (formerly `buildCauseChain` / `buildCauseContextChain`)
- `router_utility.go` — remove implicit HEAD-to-GET match from `matchesMethod`; kernel `HeadFallbackToGet` policy is now the single control point

## [v1.8.4] - 2026-04-10 - Fix XSS, Symlink Traversal, and Routing Edge Cases

### Fixed

- `exception_listener.go` — HTML error response now escapes error messages with `html.EscapeString` preventing XSS
- `exception_listener.go` — use `LoggerFromRuntime` instead of `LoggerMustFromRuntime` to prevent panic when runtime logger is not available
- `router_utility.go` — wildcard locale route attribute used `RouteAttributeName` instead of `RouteAttributeLocale`, causing catch-all wildcards named `_route` to incorrectly write to the `_locale` param
- `middleware/compression.go` — `ReadAll` error discarded partially read data; now preserves whatever was read before the error
- `middleware/cors.go` — origin matching was case-sensitive; now uses `strings.EqualFold` for case-insensitive comparison
- `middleware/rate_limit.go` — `getClientIp` now uses `RemoteAddr` only; ignores `X-Forwarded-For` and `X-Real-IP` headers to prevent IP spoofing

### Changed

- `kernel.go` — remove dead nil checks on `MatchResult` (router `Match()` always returns non-nil)
- `profiling_kernel.go` — simplify request context extraction (remove guard on always-non-nil `Attributes()`)
- `request.go` — log warning when `ParseForm` fails (previously silent)
- `url_generation_route_definition.go` — `Defaults()` and `Requirements()` now return defensive copies
- Rename `security/security_test.go` to `security/test_helper_test.go`
- Remove redundant comments from modified files

### Added

- `test_helper_test.go` — shared test runtime helper for exception listener tests
- `exception_listener_test.go`, `request_test.go`, `response_test.go`, `middleware/compression_test.go`, `middleware/cors_test.go`, `middleware/rate_limit_test.go`, `url_generation_route_definition_test.go` — new and expanded test coverage for all fixes

## [v1.8.3] - 2026-03-21 - Refactor Address Colon Check in Config

### Changed

- `config/http.go` — replaced colon-based address check with `strings.Contains` for correct host:port detection

## [v1.8.2] - 2026-03-18 - Fix HTTP HEAD Handling and Update Dev Scripts

### Fixed

- `http/router_utility.go` — aligned HEAD handling and response contract validation; prevents incorrect responses on HEAD requests

### Changed

- `internal/reflect.go` — updated type-reflection utilities
- `.dev/validate/all.sh`, `.dev/validate/mod.sh` — added `-h` help flag to validation scripts
- `.gitignore` — updated patterns

## [v1.8.1] - 2026-03-17 - Fix JSON Logging Level Label Preservation

### Fixed

- `logging/contract/level.go`, `logging/logger.go` — preserved numeric logging level labels in JSON output; `logging/json_logger_test.go` — coverage

## [v1.8.0] - 2026-03-17 - Add Module Configuration Registration and Logging Labels

### Changed

- `.dev/run-batch.sh`, `.dev/utility.sh`, `.dev/validate/all.sh` — dev scripts optimisation

### Added

- `application/contract/config_module.go` — new `ConfigModule` interface allowing modules to register configuration during application boot
- `logging/contract/config.go`, `logging/logging_config.go` — `LoggingConfig` struct and contract for customizable logging level labels
- `logging/default_logger.go`, `logging/json_logger.go`, `logging/logger.go` — updated to apply level label customization from `LoggingConfig`
- `application/application.go`, `application/application_module.go`, `application/application_new.go` — wired `ConfigModule` into the application boot sequence

## [v1.7.3] - 2026-03-05 - Add CLI Table Width Flag and Fix Docker Profile Aliases

### Fixed

- `.dev/docker/.profile` — fixed Docker `.profile` aliases in interactive shells without recursion

### Added

- `cli/output/flag.go`, `cli/output/printer_selector.go` — added `--table-width` flag for table output
- `cli/output/option.go`, `cli/output/option_parser.go`, `cli/output/standard_flag.go` — parsed and propagated new width option

## [v1.7.2] - 2026-02-28 - Add CLI Stdout/Stderr Wiring and Standardize Method Receivers

### Changed

- All `.go` files in the module — standardized all method receivers to `instance` for consistent style

### Added

- `cli/command.go`, `cli/command_output.go` — wired `stdout`/`stderr` to CLI output; print command errors with failed exit status

## [v1.7.1] - 2026-02-23 - Fix RoleVoter Auto-Upgrade to RoleHierarchyVoter

### Fixed

- `security/config/compile.go`, `security/access_decision_manager.go` — auto-upgrade `RoleVoter` to `RoleHierarchyVoter` when role hierarchy is configured

## [v1.7.0] - 2026-02-18 - Add GreaterThan and NotEmpty Validation Constraints

### Added

- `validation/constraint_greater_than.go` — new `greaterThan(value=N)` constraint with support for int, float32, float64; returns per-constraint error codes
- `validation/constraint_not_empty.go` — new `notEmpty` constraint for slices and strings; returns per-constraint error codes
- `validation/const.go`, `validation/validation_rule.go`, `validation/validator.go` — wired new constraints into the validation pipeline
- `exception/utility.go` — context-aware error wrapping helper `Wrap(ctx, err)` for exception chaining

## [v1.6.3] - 2026-02-17 - Lock-Step Release — Align with bunorm Integration Tags

Lock-step release — no `v1/` changes this cycle. Tag published to keep the core `v1` module aligned with sibling integration tags. See the `integrations/bunorm/mysql` and `integrations/bunorm/pgsql` CHANGELOGs for the provider post-build hook work captured in this cycle.

## [v1.6.2] - 2026-02-16 - Add HttpMiddlewareModule Registration Hook

### Added

- `application/contract/http_middleware_module.go` — new `HttpMiddlewareModule` interface for middleware registration
- `application/http_middleware.go`, `application/application_module.go` — wired module registration into the HTTP boot sequence

## [v1.6.1] - 2026-02-13 - Fix Token Source Panic and Add ParameterModule/ServiceModule

### Fixed

- `security/security_resolution_listener.go` — make token source resolution panic-safe and always set security context; prevents nil-pointer panics when no token source is configured

### Added

- `application/contract/parameter_module.go`, `application/contract/service_module.go` — new `ParameterModule` and `ServiceModule` interfaces for granular application boot
- `application/application.go`, `application/application_module.go` — split boot around configuration resolve; wired new module contracts into the lifecycle

## [v1.6.0] - 2026-02-11 - Lock-Step Release — Align with rueidis Integration Tag

Lock-step release — no `v1/` changes this cycle. Tag published to keep the core `v1` module aligned with the new `integrations/rueidis` module. See `integrations/rueidis/CHANGELOG.md` for the actual content.

## [v1.5.1] - 2026-02-07 - Lock-Step Release — Align with bunorm Integration Tags

Lock-step release — no `v1/` changes this cycle. Tag published to keep the core `v1` module aligned with `integrations/bunorm` sibling tags. See `integrations/bunorm/CHANGELOG.md` / `integrations/bunorm/mysql/CHANGELOG.md` / `integrations/bunorm/pgsql/CHANGELOG.md` for the actual content.

## [v1.5.0] - 2026-02-06 - Lock-Step Release — Align with bunorm/migrate Integration Tag

Lock-step release — no `v1/` changes this cycle. Tag published to keep the core `v1` module aligned with the new `integrations/bunorm/migrate` module. See `integrations/bunorm/migrate/CHANGELOG.md` for the actual content.

## [v1.4.0] - 2026-02-05 - Lock-Step Release — Align with bunorm Integration Tags

Lock-step release — no `v1/` changes this cycle. Tag published to keep the core `v1` module aligned with the new `integrations/bunorm`, `integrations/bunorm/mysql`, and `integrations/bunorm/pgsql` modules. See those CHANGELOGs for the actual content.

## [v1.3.2] - 2026-02-03 - Fix Exception CauseChain in LogContext

### Fixed

- `exception/utility.go` — included `causeChain` in `LogContext` output so error causes appear in structured log entries

## [v1.3.1] - 2026-01-30 - Fix Default Presenter Exception Override

### Fixed

- `http/exception_listener.go` — prevented default presenter from overriding exception event response in the error handling path

## [v1.3.0] - 2026-01-30 - Add Stateless Firewall and API Key Authentication

### Added

- `security/config/security_module.go`, `security/config/compile.go` — added stateless firewall support for API key authentication; kept `AddFirewall` for backwards compatibility

## [v1.2.0] - 2026-01-30 - Add Controller Autowiring and Relax Signature Validation

### Added

- `http/router_utility.go` — autowire runtime into controller parameters; relaxed controller signature validation to accept contract interfaces
- `http/request.go` — updated request helpers to support new controller signature patterns

## [v1.1.0] - 2026-01-29 - Add Route Options Contract and Group Routing API

### Added

- `http/contract/route_option.go`, `http/contract/router_group.go` — route options contract and group routing API
- `http/route.go`, `http/route_option.go`, `http/router.go`, `http/router_group.go` — implementation of route options and group routing
- `http/router_group_test.go`, `http/router_utility_test.go` — test coverage

## [v1.0.1] - 2026-01-28 - Fix Panic Cause Logging

### Fixed

- `logging/recover.go`, `logging/logger.go` — log panic causes and context chains on recovery; panics now produce structured log entries with full cause chain

## [v1.0.0] - 2026-01-17 - Initial Release

### Added

- `application/` — application container with dependency injection; `Application.Boot()` orchestrates module registration, configuration resolve, and CLI/HTTP mode dispatch
- `bag/` — parameter bag abstraction (`ParameterBag`, typed value accessors)
- `cache/` — cache abstraction (`Manager`, `InMemoryCache`, `Remember` helper with in-flight deduplication)
- `cli/` — CLI command framework with output formatting (JSON, table, list)
- `clock/` — clock abstraction with `SystemClock` and `FrozenClock` for testing
- `config/` — configuration management with placeholder resolution, environment sources, and typed sub-configs (HTTP, CLI, kernel)
- `event/` — event dispatcher with subscriber registration and priority-ordered listener dispatch
- `exception/` — exception handling with typed errors, cause chain, `LogContext`, and HTTP exception mapping
- `http/` — HTTP kernel with routing, middleware pipeline, and request/response contracts; `cors`, `rate_limit`, `compression`, and `static` middleware included
- `httpclient/` — HTTP client abstraction with per-request options and stream response support
- `logging/` — structured logging with JSON logger, emergency logger, and `recover` helper
- `runtime/` — runtime context providing access to logger, config, and container from within request scope
- `security/` — security framework with authentication, authorization, role hierarchy, firewall, and voter chain
- `serializer/` — serializer abstraction with MIME-type dispatch
- `session/` — session management with file-based and in-memory storage backends
- `validation/` — validation framework with `greaterThan`, `notEmpty`, `notBlank`, `alpha`, `alphanumeric`, `email`, `numeric`, `regex`, `minLength`, `maxLength` constraints

[Unreleased]: https://github.com/precision-soft/melody/compare/v1.14.0...HEAD

[v1.14.0]: https://github.com/precision-soft/melody/compare/v1.13.0...v1.14.0

[v1.13.0]: https://github.com/precision-soft/melody/compare/v1.12.1...v1.13.0

[v1.12.1]: https://github.com/precision-soft/melody/compare/v1.12.0...v1.12.1

[v1.12.0]: https://github.com/precision-soft/melody/compare/v1.11.0...v1.12.0

[v1.11.0]: https://github.com/precision-soft/melody/compare/v1.10.1...v1.11.0

[v1.10.1]: https://github.com/precision-soft/melody/compare/v1.10.0...v1.10.1

[v1.10.0]: https://github.com/precision-soft/melody/compare/v1.9.0...v1.10.0

[v1.9.0]: https://github.com/precision-soft/melody/compare/v1.8.4...v1.9.0

[v1.8.4]: https://github.com/precision-soft/melody/compare/v1.8.3...v1.8.4

[v1.8.3]: https://github.com/precision-soft/melody/compare/v1.8.2...v1.8.3

[v1.8.2]: https://github.com/precision-soft/melody/compare/v1.8.1...v1.8.2

[v1.8.1]: https://github.com/precision-soft/melody/compare/v1.8.0...v1.8.1

[v1.8.0]: https://github.com/precision-soft/melody/compare/v1.7.3...v1.8.0

[v1.7.3]: https://github.com/precision-soft/melody/compare/v1.7.2...v1.7.3

[v1.7.2]: https://github.com/precision-soft/melody/compare/v1.7.1...v1.7.2

[v1.7.1]: https://github.com/precision-soft/melody/compare/v1.7.0...v1.7.1

[v1.7.0]: https://github.com/precision-soft/melody/compare/v1.6.3...v1.7.0

[v1.6.3]: https://github.com/precision-soft/melody/compare/v1.6.2...v1.6.3

[v1.6.2]: https://github.com/precision-soft/melody/compare/v1.6.1...v1.6.2

[v1.6.1]: https://github.com/precision-soft/melody/compare/v1.6.0...v1.6.1

[v1.6.0]: https://github.com/precision-soft/melody/compare/v1.5.1...v1.6.0

[v1.5.1]: https://github.com/precision-soft/melody/compare/v1.5.0...v1.5.1

[v1.5.0]: https://github.com/precision-soft/melody/compare/v1.4.0...v1.5.0

[v1.4.0]: https://github.com/precision-soft/melody/compare/v1.3.2...v1.4.0

[v1.3.2]: https://github.com/precision-soft/melody/compare/v1.3.1...v1.3.2

[v1.3.1]: https://github.com/precision-soft/melody/compare/v1.3.0...v1.3.1

[v1.3.0]: https://github.com/precision-soft/melody/compare/v1.2.0...v1.3.0

[v1.2.0]: https://github.com/precision-soft/melody/compare/v1.1.0...v1.2.0

[v1.1.0]: https://github.com/precision-soft/melody/compare/v1.0.1...v1.1.0

[v1.0.1]: https://github.com/precision-soft/melody/compare/v1.0.0...v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/v1.0.0
