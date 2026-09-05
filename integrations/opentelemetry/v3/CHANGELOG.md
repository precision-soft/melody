# Changelog

All notable changes to `precision-soft/melody/integrations/opentelemetry` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- `module.go` — **Behavioural change**: a nil entry in `ModuleConfig.Middlewares` or `ModuleConfig.HandlerDecorators` is refused at boot instead of skipped. A skipped observability middleware has no later consumer to fail loudly — the typical source is a discarded constructor error (`middleware, _ := NewMetricsMiddleware(meter)`), and the app then serves traffic uninstrumented while the operator reads an empty-but-healthy dashboard with nothing to distinguish "no traffic" from "not measured"
- `metrics_middleware.go` — **Behavioural change**: a handler that returns an error is recorded at the client-facing status the kernel derives from it, not always `500`. A deliberate `404` or `403` carried as an `HttpException` used to graph as a server error under `http.server.request.*`, so a route whose normal contract is a 4xx read as 100% 5xx; only a non-http error or a 5xx exception is recorded as `500` now

### Fixed

- `tracing_middleware.go` — a handler error with no response carries the client-facing status onto the span's `http.response.status_code` instead of omitting it. The attribute was set only from a live response, so a handled 4xx produced a span marked `Error` with no status code at all, and a trace query filtering on `http.response.status_code` lost every errored request
- `otlp/tracer_provider.go` — `Config` redacts its `Headers` field on every `fmt` verb through new `String` and `Format` methods. `Headers` is exported and carries the collector auth token, so a plain `%v` of the config — dropped into a log or an error context — printed the credential in the clear; the config now renders its safe fields and `Headers:[redacted N]`, mirroring the `encrypt` key provider
- `otlp/tracer_provider.go` — the grpc exporter dials its channel with the service config lookup disabled. grpc's dns resolver asks for a TXT record beside the address records of every new channel, melody publishes no service config that way, and the channel opens lazily on the first export: on a resolver that answers the address at once and the TXT record never — docker's embedded dns for a container name, measured at five seconds against three milliseconds — a shutdown flush that was the one to open the channel spent the tracer provider's whole close budget on that record and reported a reachable collector as unreachable, so every one-signal shutdown of the example exited non-zero on such a host
- `metrics_middleware.go` — the per-route instruments read the status a handler commits directly to the response writer. A handler that returns a nil response after writing its own status — the streaming/proxy shape this package's own `MetricsRouteHandler` and the websocket bridge both use — recorded the constructor's `200` whatever was written, so a route failing 100% of the time could graph as 100% success under `http.server.request.*`; a hijacked connection on that path records `101` like the lifecycle seam does
- `metrics_middleware.go`, `tracing_middleware.go` — a typed-nil concrete response no longer panics the observability layer: `nil != response` let a handler's `var resp *SomeResponse; return resp, nil` through to the `StatusCode()` dereference, and the panic was then charged to the middleware while the defect sat in the handler; the typed nil now reads as an absent response
- `otlp/tracer_provider.go` — a negative or NaN `SampleRatio` is refused at construction instead of silently inverting to `AlwaysSample`. A negative value is the natural "tracing off" sentinel and NaN is the shape of a failed parse; both fell outside the documented `(0,1)` window check and produced 100% export exactly when the operator asked for none
- a hijacked connection is recorded as `101 Switching Protocols` instead of the default `200`, so a websocket upgrade that lives for hours no longer lands in the same request-duration series as an ordinary request and skews the latency distribution

### Security

- `go.mod` — `google.golang.org/grpc` is required at v1.82.1, the fix for [GO-2026-6061](https://pkg.go.dev/vuln/GO-2026-6061), which govulncheck reports as reachable from this module through the otlp exporter path. The dependency pinning policy keeps the oldest version that compiles, and a reachable advisory is the exception that policy exists to admit

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
