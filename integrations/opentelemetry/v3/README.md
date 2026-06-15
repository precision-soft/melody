# Melody OpenTelemetry integration (v3)

HTTP observability for Melody: distributed tracing and Prometheus metrics as HTTP middlewares, built on [`go.opentelemetry.io/otel`](https://github.com/open-telemetry/opentelemetry-go).

Structured logging already exists in core Melody; this integration adds traces and metrics.

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

metricsMiddleware, _ := opentelemetry.NewMetricsMiddleware(meter)
// register metricsMiddleware via RegisterHttpMiddlewares

// expose the registry; e.g. route GET /metrics -> opentelemetry.MetricsHandler(registry)
```

`NewMetricsMiddleware` records `http.server.request.count` and `http.server.request.duration` (ms) with `http.request.method`, `http.route`, and `http.response.status_code` attributes.

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
- This integration provides HTTP traces and metrics; exporting traces to a collector (OTLP) is configured by the application on its `TracerProvider`.
- Tests run fully in-process (in-memory span recorder + Prometheus registry); no collector is required.
