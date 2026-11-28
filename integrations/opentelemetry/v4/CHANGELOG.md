# Changelog

All notable changes to `precision-soft/melody/integrations/opentelemetry/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `otlp/insecure_collector.go`, `otlp/module.go`, `otlp/tracer_provider.go`: the module declares the boot warning `opentelemetry.otlp.insecureCollector` (`InsecureCollectorBootWarningName`) and adds it, naming the endpoint, when `Config.Insecure` exports to a collector off this machine, anything but `localhost` or a loopback address, and `otlp.NewTracerProvider` called outside the module writes it in the runtime's journal; `MELODY_BOOT_WARNINGS_IGNORED` may list the code.
- This major includes every change of the third major up to v3.2.0; that history is in the third major's changelog.
- `otlp/sampling.go`, `otlp/module.go`: `otlp.Sampling`, built by `SampleRatio` and `SampleNone`, its zero value sampling every trace, `SampleNone` sampling no trace that starts here for the first time, a sampled inbound parent still deciding for its trace; `ModuleConfig.ShutdownTimeout` bounds the provider's shutdown when its close carries no deadline, 5s when left out as the constant was, at least 1ms, unlimited refused, a refusal failing `RegisterServices`.

### Changed

- **Breaking** — `metrics_middleware.go`, `handler_decorator.go`: the request metrics follow the HTTP semantic conventions: `http.server.request.duration` in seconds on `DefaultDurationBoundaries()`, a function answering a fresh copy, `MetricsMiddlewareConfig.DurationBoundaries` the door, an empty list refused, with `url.scheme`, the scheme the kernel resolved under its forwarded-headers policy, the connection's when none was resolved, and an integer `http.response.status_code`; the lifecycle seam records `melody.http.server.lifecycle.duration` the same way, its `url.scheme` the connection's, `HandlerDecoratorConfig.DurationBoundaries` its door, an empty list or one without a `Meter` refused; the counters `http.server.request.count` and `http.server.lifecycle.request.count` are removed, dashboards reading them or the millisecond histograms go empty. See `UPGRADE.md`.
- **Breaking** — `prometheus.go`, `tracing_middleware.go`, `metrics_middleware.go`, `handler_decorator.go`, `module.go`, `otlp/module.go`: `NewPrometheusMetrics(PrometheusConfig{MeterName})` answers a handle with `Meter()`, `Handler()` and `CloseWithContext`, closed once its shutdown was entered, so a second close answers nil even after a failed one, `ModuleConfig.Prometheus` registering it for the teardown, where `NewPrometheusMeter`, `MetricsHandler` and `ModuleConfig.MetricsHandler` are removed; `NewTracingMiddleware(tracer, TracingMiddlewareConfig{Propagator})`, `NewMetricsMiddleware(meter, MetricsMiddlewareConfig)` and `NewHandlerDecorator(tracer, HandlerDecoratorConfig)` take the mandatory argument first and answer an error, a nil tracer no longer a panic. See `UPGRADE.md`.
- **Breaking** — `otlp/module.go`: `ServiceTracerProvider` is `service.opentelemetry.otlp.tracer_provider`, where it was `opentelemetry.otlp.tracer_provider`, the container's one service-name grammar; a name typed as a string re-keys.
- **Breaking** with the framework: the fourth major's runtime is read from `v4/container/contract` and built through `v4/container/runtime`, a runtime resolving through its scope only; `runtime.New` and `Runtime.Container()` are gone. See the framework's `UPGRADE.md`.
- **Behavioural change** — `metrics_middleware.go`: a handler error carrying `exception.MarkInfrastructureFailure` is labelled `503`, the status the fourth major's error renderer answers it with, where it was labelled `500`, or the store's own status beneath the mark; an http status around the marked cause keeps its own label.
- **Behavioural change** — `module.go`: `RegisterHttpRoutes` refuses a half-declared metrics route, naming the field: a `MetricsPath` with no `Prometheus`, a `Prometheus` with no `MetricsPath`, and a `MetricsRouteName` with neither, where it registered no route; leaving all three out still registers none.
- `otlp/module.go`: `RegisterHttpMiddlewares` resolves the tracer provider through `Kernel.BootRuntime()`, so the provider reads `KindBoot` and dials its exporter under the boot runtime.
- **Breaking** — `otlp/tracer_provider.go`: `NewTracerProvider(runtimeInstance, config)` takes the runtime in place of a context: a nil runtime is refused, and a runtime whose context is already done is refused before the exporter is dialled; the module's provider hands it `resolver.Runtime()`.
- `tracing_middleware.go`: the log records are built through `exception.LogContext(err, exception.LogContextOptions{...})`, the fourth major's form, which takes the extra context in a struct; nothing of this module's own surface changes.
- **Breaking** — `otlp/tracer_provider.go`, `otlp/module.go`: the collector's endpoint is `NewTracerProvider(runtimeInstance, endpoint, config)`'s argument, an empty one refused naming `argument: endpoint`, and `ModuleConfig.Endpoint`, where it was `Config.Endpoint`, which is removed and no longer printed by `Config`'s rendering.
- **Breaking** — `otlp/tracer_provider.go`: `Config.SampleRatio` is replaced by `Config.Sampling`; a ratio of 0 is refused where it sampled every trace, one above 1 is refused where it was read as 1, and NaN stays refused, each naming `Config.Sampling`. `Config.BatchTimeout` is a `bound.Duration`, 5s when left out, at least 1ms, a zero or unlimited one refused where a non-positive one took 5s; an empty endpoint names `Config.Endpoint`.
- `otlp/insecure_collector.go`: the warning's code is written under `applicationcontract.BootWarningContextKey`, the framework's key, in place of a private copy of it.

### Removed

- **Breaking** — `otlp/module.go`: `ModuleConfig.ShutdownTimeout` is removed; the provider's shutdown runs under the teardown's context, deadline or none, so `MELODY_TEARDOWN_TIMEOUT` is the one bound and a declared unlimited teardown stays unlimited.
- **Breaking** — `prometheus.go`: `NewMetricsMiddlewareWithPrometheus` is removed; `NewPrometheusMetrics`, its `Meter()` handed to `NewMetricsMiddleware(meter, MetricsMiddlewareConfig{})` and its `Handler()` compose the same three parts.

### Fixed

- `tracing_middleware.go`: a span already in the request's context, the lifecycle span of `NewHandlerDecorator`, stays the parent of the middleware's span, where a request carrying `traceparent` had the remote span extracted over it and the two spans became siblings.
- metrics: a handler's error whose `Unwrap` chain reaches itself no longer holds the request in the status classification, and a writer's flush or hijack failure of that shape no longer holds the response writer.

### Security

- `golang.org/x/net`, which the otlp exporters and grpc require, is raised to `v0.60.0`, the version that fixes GO-2026-6603, GO-2026-6611, GO-2026-6612 and GO-2026-6617, and the module's go directive to `1.26.0`, which that version requires; the third major keeps `v0.58.0` and the go 1.25 floor.
- `go.mod`: `google.golang.org/grpc` v1.83.2, past the version the advisory names, which concerns the xDS server this module does not run.
