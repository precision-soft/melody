# Changelog

All notable changes to `precision-soft/melody/integrations/opentelemetry` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.1.0] - 2026-07-06 - Lifecycle Handler Decorator and OTLP Trace Export

### Added

- `handler_decorator.go` — `NewHandlerDecorator(HandlerDecoratorConfig{Tracer, Propagator, Meter})` builds the outermost observability seam: registered through the new core `Application.RegisterHttpHandlerDecorator` (or `ModuleConfig.HandlerDecorators` on this module), it wraps the full `nethttp.Handler` the http kernel produces, so requests the middlewares never observe — security denials and other `kernel.request` short-circuits, listener-written responses, the panic-recovery path — now produce a lifecycle span (and, with a `Meter`, lifecycle count/duration metrics under distinct `http.server.lifecycle.request.*` instrument names, so routed requests are not double-counted). The routed tracing middleware's span becomes a child of the lifecycle span through the injected context. The status-recording writer keeps satisfying `http.Flusher`/`http.Hijacker` (mirroring the kernel's recording writer), so SSE and WebSocket upgrades pass through unchanged.
- `module.go` — `ModuleConfig.HandlerDecorators`: the module now implements the core `HttpHandlerDecoratorModule` hook and registers the configured decorators as outermost wrappers.
- `otlp/` — new opt-in subpackage that exports spans to an OTLP collector, kept separate so metrics-only consumers of the root package do not pull the OTLP/gRPC dependencies into their build. `otlp.NewTracerProvider(ctx, otlp.Config{Endpoint, Protocol, ServiceName, ServiceVersion, SampleRatio, Headers, Insecure, BatchTimeout})` builds a batching `TracerProvider` over the grpc (default, collector `:4317`) or http/protobuf (`:4318`) exporter. `otlp.NewModule(otlp.ModuleConfig{Config, TracerName, Propagator})` is the plug-and-play facade — registered with one `app.RegisterModule(...)`, it builds the provider, installs `NewTracingMiddleware`, and registers the provider under `otlp.ServiceTracerProvider` as a `Close()`-able container service so the application's shutdown flushes pending spans. The root package keeps `NewTracingMiddleware(tracer, propagator)` for bring-your-own-tracer wiring.

### Fixed

- `tracing_middleware.go`, `metrics_middleware.go` — `NewTracingMiddleware` and `NewMetricsMiddleware` now reject a nil tracer/meter at construction instead of nil-panicking deep inside the middleware chain on the first request. `NewTracingMiddleware` has no error return, so it panics with a clear cause (matching the framework's constructor-of-a-required-dependency idiom); `NewMetricsMiddleware` returns an error. Aligns both with `NewHandlerDecorator`, which already rejected a nil `Tracer`.

## [v3.0.0] - 2026-06-16 - Initial Release — HTTP Tracing and Prometheus Metrics

### Added

- Initial Melody v3 binding of the OpenTelemetry integration — HTTP tracing and Prometheus metrics middlewares on `go.opentelemetry.io/otel`. Developed v3-first; v1 and v2 bindings to follow.
- `module.go` — `NewModule(ModuleConfig{Middlewares, MetricsHandler, MetricsPath, MetricsRouteName})` self-registering application module that `Use`s the tracing/metrics middlewares and registers the Prometheus metrics route in one `app.RegisterModule(...)`; plus `MetricsRouteHandler(nethttp.Handler)` adapting a standard handler to a melody route handler. The metrics route is skipped when no handler or path is configured.
- `prometheus.go` — `NewMetricsMiddlewareWithPrometheus(meterName)` builds the Prometheus meter and the metrics middleware and returns both the middleware and the `/metrics` HTTP handler in one call, so userland wires metrics without assembling the meter by hand.
- `tracing_middleware.go` — `NewTracingMiddleware(tracer, propagator)`: W3C TraceContext extraction (default), one server span per request named `<METHOD> <route>`, span context injected into the downstream runtime, method/route/status attributes, error status on handler error or 5xx.
- `metrics_middleware.go` — `NewMetricsMiddleware(meter)`: `http.server.request.count` counter and `http.server.request.duration` (ms) histogram, attributed by method/route/status; route label bounded to the matched pattern (`unmatched` otherwise).
- `prometheus.go` — `NewPrometheusMeter(name)` (OTel Prometheus exporter + meter provider + registry) and `MetricsHandler(registry)` for a `/metrics` endpoint.
- `opentelemetry_test.go`, `prometheus_test.go`, `metrics_middleware_test.go`, `tracing_middleware_test.go` — in-process tests (in-memory span recorder + Prometheus registry); no collector required.

### Fixed

- `metrics_middleware.go` + `tracing_middleware.go` — the `http.request.method` metric label and the span name now normalise a non-standard HTTP method to the OpenTelemetry `_OTHER` sentinel. Go's HTTP server accepts any RFC 7230 token as a method, so an unauthenticated caller emitting many distinct methods would otherwise create unbounded metric time-series and span names (an observability denial of service); only the nine standard verbs are kept verbatim.

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/opentelemetry/v3.0.0

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/opentelemetry/v3.1.0...HEAD

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/opentelemetry/v3.0.0...integrations/opentelemetry/v3.1.0
