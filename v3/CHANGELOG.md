# Changelog

All notable changes to `precision-soft/melody/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v3.1.4] - 2026-04-10

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

## [v3.1.3] - 2026-03-21

### Changed

- Replace address colon check with `strings.Contains`

## [v3.1.2] - 2026-03-18

### Fixed

- Align HEAD handling and response contract validation

## [v3.1.1] - 2026-03-17

### Fixed

- Preserve numeric logging level labels in JSON output

## [v3.1.0] - 2026-03-17

### Added

- Module configuration registration and customizable logging level labels

## [v3.0.1] - 2026-03-08

### Changed

- Go module tidy

## [v3.0.0] - 2026-03-08

### Added

- Introduce Melody v3 module (`github.com/precision-soft/melody/v3`)
- Application context in constructor
- `ServiceModule` simplification

[v3.1.4]: https://github.com/precision-soft/melody/compare/v3.1.3...v3.1.4

[v3.1.3]: https://github.com/precision-soft/melody/compare/v3.1.2...v3.1.3

[v3.1.2]: https://github.com/precision-soft/melody/compare/v3.1.1...v3.1.2

[v3.1.1]: https://github.com/precision-soft/melody/compare/v3.1.0...v3.1.1

[v3.1.0]: https://github.com/precision-soft/melody/compare/v3.0.1...v3.1.0

[v3.0.1]: https://github.com/precision-soft/melody/compare/v3.0.0...v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/v3.0.0
