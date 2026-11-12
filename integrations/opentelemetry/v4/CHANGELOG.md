# Changelog

All notable changes to `precision-soft/melody/integrations/opentelemetry/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.2.0; that history is in the third major's changelog.
- `otlp/sampling.go`, `otlp/module.go`: `otlp.Sampling`, built by `SampleRatio` and `SampleNone`, its zero value sampling every trace, `SampleNone` sampling no trace that starts here for the first time, a sampled inbound parent still deciding for its trace; `ModuleConfig.ShutdownTimeout` bounds the provider's shutdown when its close carries no deadline, 5s when left out as the constant was, at least 1ms, unlimited refused, a refusal failing `RegisterServices`.

### Changed

- **Behavioural change** — `metrics_middleware.go`: a handler error carrying `exception.MarkInfrastructureFailure` is labelled `503`, the status the fourth major's error renderer answers it with, where it was labelled `500`, or the store's own status beneath the mark; an http status around the marked cause keeps its own label.
- **Behavioural change** — `module.go`: `RegisterHttpRoutes` refuses a half-declared metrics route, naming the field: a `MetricsPath` with no `MetricsHandler`, a `MetricsHandler` with no `MetricsPath`, and a `MetricsRouteName` with neither, where it registered no route; leaving all three out still registers none.
- `otlp/module.go`: `RegisterHttpMiddlewares` resolves the tracer provider under `Kernel.Context()`, the application's context, the fourth major's resolver form.
- `tracing_middleware.go`: the log records are built through `exception.LogContext(err, exception.LogContextConfig{...})`, the fourth major's form, which takes the extra context in a struct; nothing of this module's own surface changes.
- **Breaking** — `otlp/tracer_provider.go`: `Config.SampleRatio` is replaced by `Config.Sampling`; a ratio of 0 is refused where it sampled every trace, one above 1 is refused where it was read as 1, and NaN stays refused, each naming `Config.Sampling`. `Config.BatchTimeout` is a `bound.Duration`, 5s when left out, at least 1ms, a zero or unlimited one refused where a non-positive one took 5s; an empty endpoint names `Config.Endpoint`.

### Removed

- **Breaking** — `prometheus.go`: `NewMetricsMiddlewareWithPrometheus` is removed; `NewPrometheusMeter`, `NewMetricsMiddleware` and `MetricsHandler` compose the same three parts.

### Fixed

- `tracing_middleware.go`: a span already in the request's context, the lifecycle span of `NewHandlerDecorator`, stays the parent of the middleware's span, where a request carrying `traceparent` had the remote span extracted over it and the two spans became siblings.
- metrics: a handler's error whose `Unwrap` chain reaches itself no longer holds the request in the status classification, and a writer's flush or hijack failure of that shape no longer holds the response writer.

### Security

- `golang.org/x/net`, which the otlp exporters and grpc require, is raised to `v0.60.0`, the version that fixes GO-2026-6603, GO-2026-6611, GO-2026-6612 and GO-2026-6617, and the module's go directive to `1.26.0`, which that version requires; the third major keeps `v0.58.0` and the go 1.25 floor.
- `go.mod`: `google.golang.org/grpc` v1.83.2, past the version the advisory names, which concerns the xDS server this module does not run.
