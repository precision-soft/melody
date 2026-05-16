# Changelog

All notable changes to `precision-soft/melody/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.6.0] - 2026-05-16 - Cron Integration, Decoupled Cron Configuration, and `.example` Flat Layout

### Added

- `cli/contract/type.go` — `StringSliceFlag` type alias for `urfavecli.StringSliceFlag`; lets commands declare repeatable string-slice flags (consumed by `integrations/cron/v3` for `--heartbeat-command` and `--heartbeat-destination`) via `clicontract.StringSliceFlag` like every other flag type
- `.documentation/package/CLI.md` — listed `clicontract.StringSliceFlag` in the package surface and added a pointer to `integrations/cron/v3/` for users looking for a crontab generator
- `v3/.example/go.mod` — `v3/.example/` is now a standalone Go module (`github.com/precision-soft/melody/v3/.example`) so it can `require` framework integrations (such as `integrations/cron/v3`) without creating a cycle with the framework's own `go.mod`; local `replace` directives keep workspace builds resolving against the in-tree melody and integrations/cron checkouts
- `v3/.example/config/` package — formerly `v3/.example/bootstrap/`, now flat-layout pynbooking-style; each Module hook lives in its own file with a matching compile-time interface assertion at the bottom (`module.go` → `Module`, `parameter.go` → `ParameterModule`, `service.go` → `ServiceModule`, `security.go` → `SecurityModule`, `event.go` → `EventModule`, `middleware.go` → `HttpMiddlewareModule`, `http.go` → `HttpModule`, `cli.go` → `CliModule`, plus `cron.go` for the cron registry helper and `configure.go` for the entry point)
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
- `v3/.example/` — flattened `domain/` and `infra/` layers into top-level packages (`cache/`, `cli/`, `entity/`, `event/`, `handler/`, `page/`, `presenter/`, `repository/`, `route/`, `security/`, `service/`, `subscriber/`, `url/`). Renamed `bootstrap/` to `config/`. Mirrors pynbooking's flat layout. Domain and in-memory repositories collapsed into a single `repository/` package
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

[Unreleased]: https://github.com/precision-soft/melody/compare/v3.6.0...HEAD

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
