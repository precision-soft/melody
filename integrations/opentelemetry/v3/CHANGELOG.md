# Changelog

All notable changes to `precision-soft/melody/integrations/opentelemetry` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.0.0] - 2026-06-11 - Initial Release — HTTP Tracing and Prometheus Metrics

### Added

- Initial Melody v3 binding of the OpenTelemetry integration — HTTP tracing and Prometheus metrics middlewares on `go.opentelemetry.io/otel`. Developed v3-first; v1 and v2 bindings to follow.
- `prometheus.go` — `NewMetricsMiddlewareWithPrometheus(meterName)` builds the Prometheus meter and the metrics middleware and returns both the middleware and the `/metrics` HTTP handler in one call, so userland wires metrics without assembling the meter by hand.
- `tracing_middleware.go` — `NewTracingMiddleware(tracer, propagator)`: W3C TraceContext extraction (default), one server span per request named `<METHOD> <route>`, span context injected into the downstream runtime, method/route/status attributes, error status on handler error or 5xx.
- `metrics_middleware.go` — `NewMetricsMiddleware(meter)`: `http.server.request.count` counter and `http.server.request.duration` (ms) histogram, attributed by method/route/status; route label bounded to the matched pattern (`unmatched` otherwise).
- `prometheus.go` — `NewPrometheusMeter(name)` (OTel Prometheus exporter + meter provider + registry) and `MetricsHandler(registry)` for a `/metrics` endpoint.
- `opentelemetry_test.go`, `metrics_helper_test.go` — in-process tests (in-memory span recorder + Prometheus registry); no collector required.

### Fixed

- `metrics_middleware.go` + `tracing_middleware.go` — the `http.request.method` metric label and the span name now normalise a non-standard HTTP method to the OpenTelemetry `_OTHER` sentinel. Go's HTTP server accepts any RFC 7230 token as a method, so an unauthenticated caller emitting many distinct methods would otherwise create unbounded metric time-series and span names (an observability denial of service); only the nine standard verbs are kept verbatim.

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/opentelemetry/v3.0.0...HEAD

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/opentelemetry/v3.0.0
