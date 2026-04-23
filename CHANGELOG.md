# Changelog

All notable changes to `precision-soft/melody` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/precision-soft/melody/compare/v1.12.0...HEAD

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
