# Changelog

All notable changes to `precision-soft/melody/v2` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v2.4.1] - 2026-04-16

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
- Removed deprecated `net.Error.Temporary()` call from `integrations/bunorm/mysql` provider

### Added

- `http/static/utility_test.go` — symlink traversal rejection, absolute path rejection, parent traversal rejection, symlink within root allowed
- `cli/output/application_version_test.go` — Set/Get coverage and concurrent access race test
- `logging/emergency_logger_test.go` — singleton behavior, `Close`/recreate cycle, concurrent access
- `httpclient/http_client_test.go` — concurrent `SetHeader`/`SetBaseUrl`/`SetTimeout` with in-flight requests, `HttpClientConfig.Headers()` defensive copy
- `http/middleware/compression_test.go` — HuffmanOnly and BestCompression level boundary acceptance, out-of-range fallback to DefaultCompression
- `config/configuration_test.go` — placeholder regex rejects identifiers starting with digits, accepts letter/underscore/dotted identifiers
- `session/in_memory_storage_test.go`, `session/file_storage_test.go` — concurrent `Load`/`Save` race tests

## [v2.4.0] - 2026-04-14

### Changed

- `cache/in_memory.go` — `cleanupLoop` accepts `context.Context`; `NewInMemoryCache` creates a cancel context stored as `cleanupCancel`; `Close()` calls `cleanupCancel()` to stop the goroutine cooperatively
- `session/in_memory_storage.go` — same goroutine lifecycle improvements as `cache/in_memory.go`
- `http/request.go` — replace `log.Printf` fallback (when no runtime instance is available) with `logging.NewDefaultLogger().Warning(...)`; remove unused `"log"` import
- `cli/command.go` — remove block comments and `//nolint:errcheck` directives from `printGreenFullLine`, `printGreenStatusLine`, `printRedStatusLine` closures
- `logging/logger.go` — add GoDoc comment to `causeChainMaxDepth` constant
- `security/compiled_configuration.go` — group string fields in `CompiledFirewall` struct (`name`, `matcherDescription`, `loginPath`, `logoutPath`)

## [v2.3.0] - 2026-04-12

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

## [v2.2.4] - 2026-04-10

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

## [v2.2.3] - 2026-03-21

### Changed

- Replace address colon check with `strings.Contains`

## [v2.2.2] - 2026-03-18

### Fixed

- Align HEAD handling and response contract validation

## [v2.2.1] - 2026-03-17

### Fixed

- Preserve numeric logging level labels in JSON output

## [v2.2.0] - 2026-03-17

### Added

- Module configuration registration and customizable logging level labels

## [v2.1.3] - 2026-03-05

### Added

- CLI table width flag for table output

## [v2.1.2] - 2026-02-28

### Added

- CLI wire `stdout`/`stderr` and print command errors with failed status

### Changed

- Standardize method receivers to `instance`

## [v2.1.1] - 2026-02-23

### Fixed

- Security: auto-upgrade `RoleVoter` to `RoleHierarchyVoter` when role hierarchy is configured

## [v2.1.0] - 2026-02-18

### Added

- Validation: `greaterThan`/`notEmpty` constraints with per-constraint error codes
- Exception: context-aware wrapping helper

### Fixed

- Broken `CONTRIBUTING.md` links

## [v2.0.0] - 2026-02-17

### Added

- Introduce Melody v2 module (`github.com/precision-soft/melody/v2`)

[Unreleased]: https://github.com/precision-soft/melody/compare/v2.4.1...HEAD

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

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/v2.0.0
