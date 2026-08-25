# Melody OpenTelemetry integration (v3)

HTTP observability for Melody: distributed tracing and Prometheus metrics as HTTP middlewares, built on [`go.opentelemetry.io/otel`](https://github.com/open-telemetry/opentelemetry-go).

Structured logging already exists in core Melody; this integration adds traces and metrics.

## Version lines

This integration is v3-only (`github.com/precision-soft/melody/integrations/opentelemetry/v3`); no v1 or v2 bindings are currently planned.

## Installation

```sh
go get github.com/precision-soft/melody/integrations/opentelemetry/v3
```

```go
import opentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v3"
```

## Usage

### Metrics (Prometheus)

```go
meter, registry, meterErr := opentelemetry.NewPrometheusMeter("my-service")
if nil != meterErr {
	return meterErr
}

metricsMiddleware, middlewareErr := opentelemetry.NewMetricsMiddleware(meter)
if nil != middlewareErr {
	return middlewareErr
}
// register metricsMiddleware via RegisterHttpMiddlewares
// (the module refuses a nil middleware at boot: a discarded constructor error
// would otherwise serve traffic silently uninstrumented)

// expose the registry; e.g. route GET /metrics -> opentelemetry.MetricsHandler(registry)
```

`NewMetricsMiddleware` records `http.server.request.count` and `http.server.request.duration` (ms) with `http.request.method`, `http.route`, and `http.response.status_code` attributes. The status attribute follows the response a handler returns, or — for the nil-response streaming/proxy shape — the status the handler committed directly to the writer (`101` for a hijacked upgrade).

`NewPrometheusMeter` builds a pull-based meter: the underlying meter provider has no background goroutine and no close door is offered — it lives for the process, which is the lifetime a Prometheus registry serves anyway.

Or build the meter, the middleware, and the `/metrics` handler in one call:

```go
metricsMiddleware, metricsHandler, metricsErr := opentelemetry.NewMetricsMiddlewareWithPrometheus("my-service")
// register metricsMiddleware via RegisterHttpMiddlewares; route GET /metrics -> metricsHandler
```

### Tracing

```go
tracer := tracerProvider.Tracer("my-service") // your configured *sdktrace.TracerProvider
tracingMiddleware := opentelemetry.NewTracingMiddleware(tracer, nil) // nil -> W3C TraceContext propagation
```

The tracing middleware extracts the incoming trace context from request headers, starts a server span per request (named `<METHOD> <route>`), injects the span context into the runtime passed downstream, records method/route/status attributes, and marks the span as errored on a handler error or a 5xx response.

### OTLP export

The `TracerProvider` the tracing middleware needs is not something you have to assemble yourself: the [`otlp`](./otlp) subpackage ships one wired to an OTLP exporter.

[`otlp.NewTracerProvider(ctx, otlp.Config{...})`](./otlp/tracer_provider.go) returns a batching `*sdktrace.TracerProvider`. The caller then owns its lifecycle — `Shutdown` must run on application exit to flush pending spans. `Config.SampleRatio` accepts `(0,1)` to keep that fraction and `0` / `>=1` to keep everything; a negative or NaN ratio is refused at construction, because both used to fall through to `AlwaysSample`.

[`otlp.NewModule`](./otlp/module.go) is the plug-and-play alternative and is the recommended path: it registers the provider as the container service `otlp.ServiceTracerProvider` (`"opentelemetry.otlp.tracer_provider"`) wrapped in a `Close()`-able handle, so the container's shutdown flushes for you, and it installs the tracing middleware itself.

```go
app.RegisterModule(otlp.NewModule(otlp.ModuleConfig{
    Config: otlp.Config{
        Endpoint:    "otel-collector:4317",
        ServiceName: "my-service",
        Insecure:    true,
    },
}))
```

[`otlp.Config`](./otlp/tracer_provider.go):

| Field            | Meaning                                                                                                         | Default           |
|------------------|-----------------------------------------------------------------------------------------------------------------|-------------------|
| `Endpoint`       | `host:port`, no scheme. **Required** — an empty value returns `otlp tracer provider endpoint is required`       | —                 |
| `Protocol`       | `otlp.ProtocolGrpc` (`"grpc"`, collector port 4317) or `otlp.ProtocolHttp` (`"http"`, http/protobuf, port 4318) | `grpc`            |
| `ServiceName`    | the `service.name` resource attribute                                                                           | `melody`          |
| `ServiceVersion` | the `service.version` resource attribute                                                                        | empty             |
| `SampleRatio`    | in `(0,1)` keeps that fraction of traces (parent-based); `0` or `>= 1` samples everything                       | sample everything |
| `Headers`        | extra exporter headers (for example a vendor auth token)                                                        | none              |
| `Insecure`       | skip transport security — for a collector on the local network                                                  | `false`           |
| `BatchTimeout`   | batch span processor flush interval                                                                             | `5s`              |

`ModuleConfig` additionally takes `TracerName` (default `melody`) and `Propagator` (nil selects W3C TraceContext). The endpoint and its credentials are deployment-owned, so read them from a parameter or `.env` rather than hardcoding them.

### Register as a module

Bundle the middlewares and the `/metrics` route as a self-registering application module — one `RegisterModule` call `Use`s the middlewares and registers the metrics route (`MetricsRouteHandler` adapts the standard handler):

```go
app.RegisterModule(opentelemetry.NewModule(opentelemetry.ModuleConfig{
    Middlewares:    []httpcontract.Middleware{metricsMiddleware, tracingMiddleware},
    MetricsHandler: metricsHandler,
    MetricsPath:    "/metrics",
}))
```

The metrics route is skipped when no handler or path is configured.

## Footguns & caveats

- The route attribute uses the matched route pattern to keep metric cardinality bounded; unmatched requests are labelled `unmatched`.
- The tracing middleware replaces the downstream runtime with one carrying the span context, so handlers and nested spans link correctly.
- The root package provides HTTP traces and metrics only — it holds no exporter. Export to a collector comes from the [`otlp`](./otlp) subpackage (see [OTLP export](#otlp-export)), or from a `TracerProvider` the application configures itself.
- The OTLP `TracerProvider` must be shut down to flush pending spans. `otlp.NewModule` delegates that to the container's shutdown; a hand-built `otlp.NewTracerProvider` leaves it to the caller, and skipping it silently drops the last batch.
- Tests run fully in-process (in-memory span recorder + Prometheus registry); no collector is required.
