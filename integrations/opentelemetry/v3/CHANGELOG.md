# Changelog

All notable changes to `precision-soft/melody/integrations/opentelemetry` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.2.0] - 2026-10-19 - Client-Facing Status Recording, Bounded Flush on Teardown, Refused Nil Middlewares and Redacted Exporter Headers

This block is the condensed record of the release; the uncondensed text of its entries is in this file at commit `234f9cb0`, <https://github.com/precision-soft/melody/blob/234f9cb054e644bb44d32d76d37548d69d7a5b97/integrations/opentelemetry/v3/CHANGELOG.md>.

### Added

- `module.go` — the OTLP tracer provider handle carries `CloseWithContext(ctx)`, the optional door the container's ordered teardown prefers, so the span flush runs under the deadline the operator declared for the whole shutdown: the handle adapts `TracerProvider.Shutdown(ctx)` to a `Close() error` the container could see, and the five seconds it invented were neither configurable nor visible to the shutdown; the plain `Close()` keeps them, named `unbudgetedShutdownGrace`, for a caller with no budget

### Changed

- documentation: the readme documents `NewHandlerDecorator`, the lifecycle seam that wraps the whole handler the http kernel builds, the required tracer, the optional meter and its `http.server.lifecycle.*` instruments, and registration through `Application.RegisterHttpHandlerDecorator` or `ModuleConfig.HandlerDecorators`, where only the GoDoc named it
- **Behavioural change**: `module.go` — a nil entry in `ModuleConfig.Middlewares` or `ModuleConfig.HandlerDecorators` is refused at boot instead of skipped: a skipped observability middleware has no later consumer to fail loudly, the typical source being a discarded constructor error (`middleware, _ := NewMetricsMiddleware(meter)`), and the app served traffic uninstrumented under an empty-but-healthy dashboard
- **Behavioural change**: `metrics_middleware.go` — a handler that returns an error is recorded at the client-facing status the kernel derives from it, not always `500`, where a deliberate `404` or `403` carried as an `HttpException` graphed as a server error under `http.server.request.*`; an `HttpException` is recorded at its own status, a body past its limit as `413`, and only an error that is neither as `500`
- **Behavioural change**: `metrics_middleware.go`, `tracing_middleware.go` — a status the handler already committed to the writer is the one recorded, over a response or an error the handler returns after it, since the client received the committed status: a handler that wrote `202` and then returned a `500` response or an error is recorded as `202`, the span marked in error only when the status the client received is a `5xx`, the handler's error staying the span's exception event
- **Behavioural change**: `tracing_middleware.go` — a handler that returns both a response and an error is traced at the status the kernel answers, the error's, as the metrics record it, where the span carried the returned response's status

### Fixed

- documentation: the GoDoc of `otlp.Config.SampleRatio` and the README say the sampler is parent-based, so at an untrusted edge an inbound `traceparent` chooses the sampling; the two grpc requirement entries are one, at v1.83.1, naming both advisories it fixes; and the README says which status the metrics and the span record: a status the handler committed to the writer first, then the handler's error, then its response, then what it wrote
- `metrics_middleware.go`, `tracing_middleware.go` — the status recorded is the one the client receives: a 5xx `HttpException` a handler returned graphs at its own status, a 502 as a 502, and a body past its limit as the kernel's 413, where both graphed as 500; a handler that writes the response itself carries its status onto the span and the per-route instruments, through the package's recording writer, which keeps flushing, hijacking, `ReadFrom` and the response controller reachable, where the streaming, proxy and upgrade shapes, returning neither response nor error, produced a span with no `http.response.status_code`, a streamed `503` left unmarked, and a nil response after a written status recorded the constructor's `200`, the shape this package's own `MetricsRouteHandler` and the websocket bridge use, so a route failing 100% of the time could graph as 100% success under `http.server.request.*`; a hijacked connection is recorded as `101 Switching Protocols` (`101`) on both paths,
  so a websocket upgrade that lives for hours no longer skews the request-duration series; an informational status other than `101` is forwarded without being recorded, the final status being the one the handler writes next or the implicit `200` of its first write, where a handler that sent `103 Early Hints` and then `503` was traced and counted as `103`; a handler error with no response carries the client-facing status onto the span's `http.response.status_code`, where a handled 4xx produced a span marked `Error` with no status code and a trace query filtering on it lost every errored request; the server span's status stays unset for a deliberate sub-500, a 404, a 422, the error still recorded as the span's exception event; a handler error's message is read through the exception package rather than `Error()` bare, where a typed nil panicked inside the middleware; and a typed-nil concrete response reads as an absent response, where `nil != response` let
  `var resp *SomeResponse; return resp, nil` through to the `StatusCode()` dereference and the panic was charged to the middleware
- the recording writer flushes and hijacks through net/http's response controller, where by assertion on the writer it wraps a middleware wrapper forwarding `Unwrap` alone left a stream's flush doing nothing and an upgrade refused; a flush that reached a flusher and failed, the write error of a client that has gone with the status committed, still counts as the header written, so a status the handler tries afterwards is not recorded in place of the one the connection carried
- `otlp` — the flush reserve moved onto the door the container actually calls, `CloseWithContext`, and a close reached with the deadline already spent is refused instead of being driven under it, naming the reason in the container's failure map where the vendor's bare `context deadline exceeded` stood: a reserve applied in `Close()` alone was never spent, a shutdown with no deadline handing the provider NO margin, and `TracerProvider.Shutdown` latches its shut flag through a compare-and-swap BEFORE its loop, returns at the head of the first iteration and answers `nil` to every later call, leaving the provider permanently unclosable with its batch goroutine, its ticker and its exporter connection running; in `otlp/tracer_provider.go` the grpc exporter dials its channel with the service config lookup disabled, since grpc's dns resolver asks for a TXT record melody never publishes and on a resolver answering the address at once and the TXT record never, docker's embedded dns for a
  container name (five seconds against three milliseconds), a shutdown flush that opened the channel spent the whole close budget on it and reported a reachable collector as unreachable, every one-signal shutdown of the example exiting non-zero; and a negative or NaN `SampleRatio` is refused at construction instead of silently inverting to `AlwaysSample`, both falling outside the documented `(0,1)` window check and producing 100% export exactly when the operator asked for none
- `otlp/tracer_provider.go` — `Config` redacts its `Headers` field on every `fmt` verb through new `String` and `Format` methods, rendering its safe fields and `Headers:[redacted N]`, mirroring the `encrypt` key provider, where `Headers`, exported and carrying the collector auth token, printed the credential in the clear through a plain `%v` dropped into a log or an error context

### Security

- `go.mod` — the OpenTelemetry Go family (`otel`, `otel/trace`, `otel/metric`, `otel/sdk`, `otel/sdk/metric` and the three `otlptrace` exporters) is required at v1.45.0, which fixes [GO-2026-6505](https://pkg.go.dev/vuln/GO-2026-6505): the exporter wrote its configuration, the collector endpoint url included, into info-level logs; `go.opentelemetry.io/proto/otlp` moves to v1.11.0 and the indirect requirements with it
- `go.mod` — `google.golang.org/grpc` is required at v1.83.1, which fixes [GO-2026-6061](https://pkg.go.dev/vuln/GO-2026-6061) and [GO-2026-6348](https://pkg.go.dev/vuln/GO-2026-6348) (heap exhaustion through HTTP/2 DATA frame fragmentation), reachable from this module through the otlp exporter's transport; the pinning policy keeps the oldest version that compiles, and a reachable advisory is the exception it exists to admit

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/opentelemetry/v3.2.0...HEAD
[v3.2.0]: https://github.com/precision-soft/melody/compare/integrations/opentelemetry/v3.1.0...integrations/opentelemetry/v3.2.0

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/opentelemetry/v3.0.0...integrations/opentelemetry/v3.1.0
