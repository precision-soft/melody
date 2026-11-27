# Melody OpenTelemetry integration (v4)

HTTP observability for Melody: distributed tracing and Prometheus metrics as HTTP middlewares, built on [`go.opentelemetry.io/otel`](https://github.com/open-telemetry/opentelemetry-go).

Structured logging already exists in core Melody; this integration adds traces and metrics.

## Version lines

This integration ships for the third and the fourth major (`github.com/precision-soft/melody/integrations/opentelemetry/v4`); no v1 or v2 bindings are currently planned.

## Installation

```bash
go get github.com/precision-soft/melody/integrations/opentelemetry/v4
```

```go
import opentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v4"
```

## Usage

### Metrics (Prometheus)

```go
prometheusMetrics, metricsErr := opentelemetry.NewPrometheusMetrics(opentelemetry.PrometheusConfig{MeterName: "my-service"})
if nil != metricsErr {
    return metricsErr
}

metricsMiddleware, middlewareErr := opentelemetry.NewMetricsMiddleware(prometheusMetrics.Meter(), opentelemetry.MetricsMiddlewareConfig{})
if nil != middlewareErr {
    return middlewareErr
}
/* register metricsMiddleware via RegisterHttpMiddlewares and prometheusMetrics through ModuleConfig.Prometheus; the module refuses a nil middleware at boot */
```

`NewMetricsMiddleware(meter, MetricsMiddlewareConfig{DurationBoundaries})` records the HTTP semantic conventions' `http.server.request.duration` histogram in seconds (unit `s`, exposed as `http_server_request_duration_seconds`), bucketed on `DefaultDurationBoundaries()` (`0.005` … `10`, the conventions' advisory list, a fresh copy on every call) unless `DurationBoundaries` sets others, an empty non-nil list refused since it would leave the histogram without buckets, with `http.request.method`, `http.route`, `url.scheme` (the scheme the kernel resolved under its forwarded-headers policy, the one the access log records, so a trusted proxy that terminated TLS reads `https`; a request without a resolved scheme reads its connection, `https` over TLS and `http` otherwise) and an integer `http.response.status_code`. No request counter is recorded: the histogram's count is the request count. The status attribute is the status the client receives: a status the handler already committed to the writer is recorded over anything it returns afterwards (`101` for a hijacked upgrade); otherwise, in the order the kernel answers it, a handler error decides first (an `HttpException` at its own status, a body past its limit as `413`, anything else as `500`), then the response the handler returns, and for a handler that returned neither, what it wrote. A nil meter is refused with an error.

`NewPrometheusMetrics(PrometheusConfig{MeterName})` builds a pull-based meter, its instrumentation scope the module's import path when `MeterName` is left out, and answers a handle: `Meter()`, `Handler()` (the exposition) and `CloseWithContext(closeContext)`, which shuts the meter provider down, a close context already done answered its error with the provider left open; once the shutdown was entered the handle is closed whatever it answered, since the sdk runs it once, so a second call answers nil. `ModuleConfig.Prometheus` registers the handle as `ServicePrometheusMetrics`, so the container's teardown closes it.

### Tracing

```go
tracer := tracerProvider.Tracer("my-service") // your configured *sdktrace.TracerProvider
tracingMiddleware, middlewareErr := opentelemetry.NewTracingMiddleware(tracer, opentelemetry.TracingMiddlewareConfig{}) /* a left-out Propagator is W3C TraceContext; a nil tracer is an error */
```

The tracing middleware extracts the incoming trace context from request headers, starts a server span per request (named `<METHOD> <route>`), injects the span context into the runtime passed downstream, records method/route/status attributes with the status decided as the metrics decide it — including a status the handler writes directly, as streaming, proxy and upgrade handlers do — and marks the span as errored when the status the client receives is a 5xx; a handler error is recorded on the span as an exception event either way.

### Lifecycle decorator

The middlewares see a request only once the kernel routed it, so a security denial, a response a `kernel.request` listener wrote and the panic-recovery path leave no span and no metric there. [`NewHandlerDecorator`](./handler_decorator.go) builds the outermost seam instead: it wraps the whole `nethttp.Handler` the http kernel produces.

```go
decorator, decoratorErr := opentelemetry.NewHandlerDecorator(tracer, opentelemetry.HandlerDecoratorConfig{
    Meter: meter, /* optional; a nil tracer is refused with "handler decorator tracer is nil" */
})
if nil != decoratorErr {
    return decoratorErr
}

app.RegisterHttpHandlerDecorator(decorator) // or ModuleConfig.HandlerDecorators
```

Every request, short-circuited ones included, gets a lifecycle span; the tracing middleware's span becomes its child through the context the decorator injects. `Propagator` defaults to W3C TraceContext. With a `Meter`, the decorator times every request on one histogram, `melody.http.server.lifecycle.duration`, in seconds, on the same boundaries (`HandlerDecoratorConfig.DurationBoundaries` sets others; an empty list, or one set without a `Meter`, is refused naming the field), with the method, `url.scheme` and an integer status (the lifecycle seam runs before the kernel resolves a scheme, so its `url.scheme` reads the connection alone and can say `http` behind a TLS-terminating proxy where the middleware's says `https`): a name outside the conventions' reserved `http.server.*`, distinct from the middleware's, so a routed request is not counted twice.

### OTLP export

The `TracerProvider` the tracing middleware needs is not something you have to assemble yourself: the [`otlp`](./otlp) subpackage ships one wired to an OTLP exporter.

[`otlp.NewTracerProvider(runtimeInstance, endpoint, otlp.Config{...})`](./otlp/tracer_provider.go) returns, under the runtime's context, a batching `*sdktrace.TracerProvider` exporting to `endpoint`, `host:port` without a scheme; an empty one returns `otlp tracer provider endpoint is required`, naming the argument. The caller then owns its lifecycle — `Shutdown` must run on application exit to flush pending spans. `Config.Sampling` samples every trace when left out; `otlp.SampleRatio(x)` keeps the fraction `x` in `(0, 1]` and `otlp.SampleNone()` keeps none of the traces that start here; a ratio of `0`, one above `1` and NaN are refused at construction naming `Config.Sampling`, so "none" has the one spelling. The sampler is parent-based: a client's `traceparent` sampled flag decides for the whole trace, so at an untrusted edge the decision is the client's to make — strip or ignore the inbound header there.

[`otlp.NewModule`](./otlp/module.go) is the plug-and-play alternative and is the recommended path: it registers the provider as the container service `otlp.ServiceTracerProvider` (`"service.opentelemetry.otlp.tracer_provider"`) wrapped in a closeable handle, so the container's shutdown flushes for you, and it installs the tracing middleware itself. The container reaches that handle through its context-taking close, so the flush runs under the teardown budget the application declares rather than under a figure of this module's own.

```go
app.RegisterModule(otlp.NewModule(otlp.ModuleConfig{
    Endpoint: "otel-collector:4317",
    Config: otlp.Config{
        ServiceName: "my-service",
        Insecure:    true,
    },
}))
```

[`otlp.Config`](./otlp/tracer_provider.go):

| Field            | Meaning                                                                                                         | Default           |
|------------------|-----------------------------------------------------------------------------------------------------------------|-------------------|
| `Protocol`       | `otlp.ProtocolGrpc` (`"grpc"`, collector port 4317) or `otlp.ProtocolHttp` (`"http"`, http/protobuf, port 4318) | `grpc`            |
| `ServiceName`    | the `service.name` resource attribute                                                                           | `melody`          |
| `ServiceVersion` | the `service.version` resource attribute                                                                        | empty             |
| `Sampling`       | `otlp.SampleRatio(x)` keeps the fraction `x` in `(0, 1]` (parent-based), `otlp.SampleNone()` keeps none         | sample everything |
| `Headers`        | extra exporter headers (for example a vendor auth token)                                                        | none              |
| `Insecure`       | skip transport security — for a collector on the local network                                                  | `false`           |
| `BatchTimeout`   | batch span processor flush interval, a `bound.Duration`: at least 1ms, unlimited refused                        | `5s`              |

`ModuleConfig` additionally takes `Endpoint`, the collector's `host:port` it hands `NewTracerProvider`, `TracerName` (default `melody`) and `Propagator` (nil selects W3C TraceContext). The provider's shutdown runs under the teardown's own context, `MELODY_TEARDOWN_TIMEOUT`, deadline or none. The endpoint and its credentials are deployment-owned, so read them from a parameter or `.env` rather than hardcoding them. `Config` redacts `Headers` in every `fmt` rendering (`String`/`Format`), since that field carries the collector auth token: a config dropped into a log or an error context prints `Headers:[redacted N]` beside its safe fields, never the token.

With `Insecure` set and an `Endpoint` off this machine, anything but `localhost` or a loopback address, the module adds the boot warning `opentelemetry.otlp.insecureCollector` (`otlp.InsecureCollectorBootWarningName`), naming the endpoint and never a header: the spans, their attributes and `Headers` cross the network in clear. The code is declared whether or not the export is insecure, so `MELODY_BOOT_WARNINGS_IGNORED` may list it where a collector on a trusted network is reached in clear on purpose. A provider built with `NewTracerProvider` outside the module writes no warning.

### Register as a module

Bundle the middlewares and the `/metrics` route as a self-registering application module — one `RegisterModule` call `Use`s the middlewares and registers the metrics route (`MetricsRouteHandler` adapts the standard handler):

```go
app.RegisterModule(opentelemetry.NewModule(opentelemetry.ModuleConfig{
    Middlewares: []httpcontract.Middleware{metricsMiddleware, tracingMiddleware},
    Prometheus:  prometheusMetrics,
    MetricsPath: "/metrics",
    // HandlerDecorators: []applicationcontract.HttpHandlerDecorator{decorator},
}))
```

The metrics route serves `Prometheus.Handler()` when both `Prometheus` and `MetricsPath` are set and is absent when both are left out; a half-declared one (a path with no `Prometheus`, a `Prometheus` with no path, or a `MetricsRouteName` with neither) fails the boot naming the field.

## Footguns & caveats

- The route attribute uses the matched route pattern to keep metric cardinality bounded; unmatched requests are labelled `unmatched`.
- The tracing middleware replaces the downstream runtime with one carrying the span context, so handlers and nested spans link correctly.
- The root package provides HTTP traces and metrics only — it holds no exporter. Export to a collector comes from the [`otlp`](./otlp) subpackage (see [OTLP export](#otlp-export)), or from a `TracerProvider` the application configures itself.
- The OTLP `TracerProvider` must be shut down to flush pending spans. `otlp.NewModule` delegates that to the container's shutdown; a hand-built `otlp.NewTracerProvider` leaves it to the caller, and skipping it silently drops the last batch.
- **A teardown budget already spent by the time the provider is reached means the flush does not run at all**, and the handle says so rather than attempting it. Driving the vendor's shutdown under a spent deadline latches its shut flag before it reaches a single span processor and makes every later attempt answer success, which leaves the batch goroutine, the ticker and the exporter connection running with no door left to end them. Refusing keeps the provider closable. With no deadline at all the handle applies its own five-second reserve; a declared budget is honoured whole. Components that close before this one spend from the same budget, so a broker that wedges its own close is what takes the reserve away — the container's failure map names both.
- Tests run fully in-process (in-memory span recorder + Prometheus registry); no collector is required.
