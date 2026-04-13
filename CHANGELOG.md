# Changelog

All notable changes to `precision-soft/melody` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v1.9.0] - 2026-04-12

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
- `exception/utility.go` — export `BuildCauseChain` and `BuildCauseContextChain` (formerly `buildCauseChain` / `buildCauseContextChain`)
- `logging/logger.go` — remove duplicated `buildCauseChain` / `buildCauseContextChain`; delegate to `exception.BuildCauseChain` / `exception.BuildCauseContextChain`
- `router_utility.go` — remove implicit HEAD-to-GET match from `matchesMethod`; kernel `HeadFallbackToGet` policy is now the single control point

## [v1.8.4] - 2026-04-10

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

## [v1.8.3] - 2026-03-21

### Changed

- Replace address colon check with `strings.Contains`

## [v1.8.2] - 2026-03-18

### Fixed

- Align HEAD handling and response contract validation

### Changed

- Dev scripts `-h` flag
- Update `.gitignore`

## [v1.8.1] - 2026-03-17

### Fixed

- Preserve numeric logging level labels in JSON output

## [v1.8.0] - 2026-03-17

### Added

- Module configuration registration and customizable logging level labels

### Changed

- Dev scripts optimisation

## [v1.7.3] - 2026-03-05

### Added

- CLI table width flag for table output

### Fixed

- Docker `.profile` aliases in interactive shells without recursion

## [v1.7.2] - 2026-02-28

### Added

- CLI wire `stdout`/`stderr` and print command errors with failed status

### Changed

- Standardize method receivers to `instance`

## [v1.7.1] - 2026-02-23

### Fixed

- Security: auto-upgrade `RoleVoter` to `RoleHierarchyVoter` when role hierarchy is configured

## [v1.7.0] - 2026-02-18

### Added

- Validation: `greaterThan`/`notEmpty` constraints with per-constraint error codes
- Exception: context-aware wrapping helper

## [v1.6.3] - 2026-02-17

### Changed

- Bunorm: add provider post-build hooks (mysql/pgsql)

## [v1.6.2] - 2026-02-16

### Added

- Application: `HttpMiddlewareModule` registration hook
- PostgreSQL `IsDuplicateKey` function

## [v1.6.1] - 2026-02-13

### Fixed

- Security: make token source resolution panic-safe and always set security context

### Added

- Application: `ParameterModule`/`ServiceModule` and split boot around configuration resolve

## [v1.6.0] - 2026-02-11

### Added

- Rueidis integration and submodule `go.mod` alignment for local replace

## [v1.5.1] - 2026-02-07

### Added

- Bunorm: retryable provider open and resolver logger helper

## [v1.5.0] - 2026-02-06

### Added

- Bunorm migrate: reusable Bun migration CLI commands with Go migrations

## [v1.4.0] - 2026-02-05

### Added

- Bunorm: core registry and mysql/pgsql providers

## [v1.3.2] - 2026-02-03

### Fixed

- Exception: include `causeChain` in `LogContext`

## [v1.3.1] - 2026-01-30

### Fixed

- HTTP: do not override exception event response in default presenter

## [v1.3.0] - 2026-01-30

### Added

- Security: support stateless firewalls (API key) and keep `AddFirewall`

## [v1.2.0] - 2026-01-30

### Added

- HTTP: autowire runtime into controller parameters
- HTTP: relax controller signature validation to accept contract interfaces

## [v1.1.0] - 2026-01-29

### Added

- HTTP: route options contract and group routing API

## [v1.0.1] - 2026-01-28

### Fixed

- Logging: log panic causes and contexts

## [v1.0.0] - 2026-01-17

### Added

- Initial release — Go HTTP framework with kernel, routing, middleware, request/response helpers
- Application container with dependency injection
- Bag (parameter bag) abstraction
- Cache abstraction
- CLI command framework
- Clock abstraction with frozen clock for testing
- Configuration management
- Event dispatcher
- Exception handling with typed errors
- HTTP kernel with routing, middleware pipeline, and request/response contracts
- HTTP client abstraction
- Logging with structured output
- Runtime context
- Security framework with authentication and authorization
- Serializer abstraction
- Session management
- Validation framework

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
